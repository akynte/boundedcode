package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
	"github.com/akynte/boundedcode/internal/xservice"
)

func (a *App) workspaces(ctx context.Context) (workspace.Store, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return workspace.Store{}, err
	}
	return workspace.Store{DB: s.DB}, nil
}

func (a *App) currentWorkspaceFile() string {
	return filepath.Join(a.Paths.Config, "current-workspace")
}

// resolveWorkspace picks --workspace, else the current workspace, else the
// only workspace.
func (a *App) resolveWorkspace(ctx context.Context, flag string) (workspace.Workspace, error) {
	ws, err := a.workspaces(ctx)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if flag == "" {
		if b, err := os.ReadFile(a.currentWorkspaceFile()); err == nil {
			flag = strings.TrimSpace(string(b))
		}
	}
	if flag != "" {
		return ws.Get(ctx, flag)
	}
	all, err := ws.List(ctx)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if len(all) == 1 {
		return all[0], nil
	}
	return workspace.Workspace{}, errors.New("no workspace selected: pass --workspace or run `workspace use NAME`")
}

func (a *App) intel() *cbm.Client {
	return &cbm.Client{Binary: a.Config.RepoIntel.Binary, CacheDir: filepath.Join(a.Paths.Cache, "codebase-memory")}
}

func newWorkspaceCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Aliases: []string{"ws"}, Short: "Manage multi-repository workspaces"}
	cmd.AddCommand(&cobra.Command{
		Use: "create NAME", Short: "Create a workspace and make it current", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := app.workspaces(cmd.Context())
			if err != nil {
				return err
			}
			w, err := ws.Create(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := os.WriteFile(app.currentWorkspaceFile(), []byte(w.Name+"\n"), 0o600); err != nil {
				return err
			}
			rec, _ := app.Recorder(cmd.Context())
			rec.Emit(cmd.Context(), "", "workspace.created", w)
			app.printf("created workspace %s (%s)\n", w.Name, w.ID)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "use NAME", Short: "Select the current workspace", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := app.resolveWorkspace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return os.WriteFile(app.currentWorkspaceFile(), []byte(w.Name+"\n"), 0o600)
		},
	})
	var wsFlag, repoName string
	add := &cobra.Command{
		Use: "add PATH", Short: "Add a git repository to the workspace", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := app.resolveWorkspace(cmd.Context(), wsFlag)
			if err != nil {
				return err
			}
			ws, _ := app.workspaces(cmd.Context())
			r, err := ws.AddRepo(cmd.Context(), w, args[0], repoName)
			if err != nil {
				return err
			}
			rec, _ := app.Recorder(cmd.Context())
			rec.Emit(cmd.Context(), "", "workspace.repo_added", map[string]any{"workspace": w.Name, "repo": r.Name, "path": r.Path})
			app.printf("added %s (%s) languages=%v\n", r.Name, r.Path, r.Languages)
			return nil
		},
	}
	add.Flags().StringVarP(&wsFlag, "workspace", "w", "", "workspace")
	add.Flags().StringVar(&repoName, "name", "", "repository name (default: directory name)")
	cmd.AddCommand(add)
	// repoCmd builds remove/disable/enable, which act on one repository.
	repoCmd := func(use, short string, f func(ctx context.Context, ws workspace.Store, r workspace.Repository) (string, error)) *cobra.Command {
		var wsFlag string
		c := &cobra.Command{
			Use: use + " REPO", Short: short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				w, err := app.resolveWorkspace(ctx, wsFlag)
				if err != nil {
					return err
				}
				ws, _ := app.workspaces(ctx)
				r, err := ws.Repo(ctx, w.ID, args[0])
				if err != nil {
					return err
				}
				done, err := f(ctx, ws, r)
				if err != nil {
					return fmt.Errorf("%s %s: %w", use, r.Name, err)
				}
				rec, _ := app.Recorder(ctx)
				rec.Emit(ctx, "", "workspace.repo_"+done, map[string]any{"workspace": w.Name, "repo": r.Name, "id": r.ID})
				app.printf("%s %s (%s)\n", done, r.Name, r.ID)
				return nil
			},
		}
		c.Flags().StringVarP(&wsFlag, "workspace", "w", "", "workspace")
		return c
	}
	cmd.AddCommand(
		repoCmd("remove", "Remove a repository from the workspace (refused once tasks used it; disable it instead)",
			func(ctx context.Context, ws workspace.Store, r workspace.Repository) (string, error) {
				return "removed", ws.RemoveRepo(ctx, r.ID)
			}),
		repoCmd("disable", "Exclude a repository from new tasks, indexing and queries (keeps its id, index and history)",
			func(ctx context.Context, ws workspace.Store, r workspace.Repository) (string, error) {
				return "disabled", ws.SetEnabled(ctx, r.ID, false)
			}),
		repoCmd("enable", "Re-enable a disabled repository",
			func(ctx context.Context, ws workspace.Store, r workspace.Repository) (string, error) {
				return "enabled", ws.SetEnabled(ctx, r.ID, true)
			}),
	)
	show := &cobra.Command{
		Use: "show", Short: "Show workspaces and repositories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := app.workspaces(cmd.Context())
			if err != nil {
				return err
			}
			all, err := ws.List(cmd.Context())
			if err != nil {
				return err
			}
			type view struct {
				workspace.Workspace
				Repos []workspace.Repository `json:"repos"`
			}
			var out []view
			for _, w := range all {
				repos, err := ws.AllRepos(cmd.Context(), w.ID)
				if err != nil {
					return err
				}
				out = append(out, view{w, repos})
			}
			if app.jsonOut {
				return app.printJSON(out)
			}
			cur, _ := os.ReadFile(app.currentWorkspaceFile())
			for _, v := range out {
				mark := " "
				if strings.TrimSpace(string(cur)) == v.Name {
					mark = "*"
				}
				app.printf("%s %s (%s)\n", mark, v.Name, v.ID)
				for _, r := range v.Repos {
					idx := "not indexed"
					if r.IndexedAt != "" {
						idx = "indexed " + r.IndexedAt[:19]
					}
					if !r.Enabled {
						idx += " (disabled)"
					}
					app.printf("    %-24s %-50s %v %s\n", r.Name, r.Path, r.Languages, idx)
				}
			}
			return nil
		},
	}
	cmd.AddCommand(show)
	return cmd
}

func newIndexCmd(app *App) *cobra.Command {
	var wsFlag, mode string
	cmd := &cobra.Command{
		Use:   "index [REPO...]",
		Short: "Index workspace repositories with repository intelligence",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := app.resolveWorkspace(ctx, wsFlag)
			if err != nil {
				return err
			}
			ws, _ := app.workspaces(ctx)
			repos, err := ws.Repos(ctx, w.ID)
			if err != nil {
				return err
			}
			for _, a := range args {
				if !slices.ContainsFunc(repos, func(r workspace.Repository) bool { return r.Name == a }) {
					return fmt.Errorf("repository %q is not an enabled repository of %s (see `workspace show`)", a, w.Name)
				}
			}
			rec, _ := app.Recorder(ctx)
			st, _ := app.Store(ctx)
			intel := app.intel()
			if err := intel.Open(ctx); err != nil {
				app.Log.Warn("persistent MCP session unavailable; using one-shot CLI", "err", err)
			}
			defer intel.Close()
			for _, r := range repos {
				if len(args) > 0 && !contains(args, r.Name) {
					continue
				}
				project := w.Name + "." + r.Name
				t0 := time.Now()
				res, err := intel.Index(ctx, r.Path, project, mode)
				if err != nil {
					return fmt.Errorf("index %s: %w", r.Name, err)
				}
				if err := ws.MarkIndexed(ctx, r.ID, res.Project); err != nil {
					return err
				}
				line := fmt.Sprintf("%-24s graph: nodes=%d edges=%d", r.Name, res.Nodes, res.Edges)
				if app.Config.RepoIntel.CrossService {
					eps, diags, err := xservice.Scan(r.Name, r.Path, xservice.ScanOptions{})
					if err != nil {
						return fmt.Errorf("cross-service scan %s: %w", r.Name, err)
					}
					if err := xservice.SaveRepo(ctx, st.DB, w.ID, r.ID, eps); err != nil {
						return err
					}
					line += fmt.Sprintf("  contracts: %d endpoints (%d diagnostics)", len(eps), len(diags))
				}
				rec.Emit(ctx, "", "repointel.indexed", map[string]any{"repo": r.Name, "project": res.Project, "nodes": res.Nodes, "edges": res.Edges, "seconds": time.Since(t0).Seconds()})
				app.printf("%s  %.1fs\n", line, time.Since(t0).Seconds())
			}
			if app.Config.RepoIntel.CrossService {
				eps, err := xservice.LoadWorkspace(ctx, st.DB, w.ID)
				if err != nil {
					return err
				}
				app.printf("cross-service links in workspace %s: %d (see `intel links`)\n", w.Name, len(xservice.LinkAll(eps, xservice.LinkOptions{})))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&wsFlag, "workspace", "w", "", "workspace")
	cmd.Flags().StringVar(&mode, "mode", "full", "index mode: full|moderate|fast|cross-repo-intelligence")
	return cmd
}

func newIntelCmd(app *App) *cobra.Command {
	var wsFlag, repoFlag string
	cmd := &cobra.Command{Use: "intel", Short: "Query repository intelligence (search, trace, impact, snippet)"}
	cmd.PersistentFlags().StringVarP(&wsFlag, "workspace", "w", "", "workspace")
	cmd.PersistentFlags().StringVarP(&repoFlag, "repo", "r", "", "repository (default: all indexed repos)")
	run := func(f func(ctx context.Context, project string) (string, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w, err := app.resolveWorkspace(ctx, wsFlag)
			if err != nil {
				return err
			}
			ws, _ := app.workspaces(ctx)
			repos, err := ws.Repos(ctx, w.ID)
			if err != nil {
				return err
			}
			n := 0
			for _, r := range repos {
				if (repoFlag != "" && r.Name != repoFlag) || r.IndexProject == "" {
					continue
				}
				n++
				out, err := f(ctx, r.IndexProject)
				if err != nil {
					return err
				}
				app.printf("## %s\n%s\n", r.Name, out)
			}
			if n == 0 {
				return fmt.Errorf("no indexed repositories matched: %w", store.ErrNotFound)
			}
			return nil
		}
	}
	cmd.AddCommand(&cobra.Command{Use: "search QUERY", Args: cobra.ExactArgs(1), Short: "Search symbols",
		RunE: func(c *cobra.Command, a []string) error {
			return run(func(ctx context.Context, p string) (string, error) { return app.intel().Search(ctx, p, a[0], 20) })(c, a)
		}})
	cmd.AddCommand(&cobra.Command{Use: "trace FUNCTION", Args: cobra.ExactArgs(1), Short: "Callers and callees",
		RunE: func(c *cobra.Command, a []string) error {
			return run(func(ctx context.Context, p string) (string, error) { return app.intel().Trace(ctx, p, a[0], "both", 2) })(c, a)
		}})
	cmd.AddCommand(&cobra.Command{Use: "snippet NAME", Args: cobra.ExactArgs(1), Short: "Source of a symbol",
		RunE: func(c *cobra.Command, a []string) error {
			return run(func(ctx context.Context, p string) (string, error) { return app.intel().Snippet(ctx, p, a[0]) })(c, a)
		}})
	addSerenaIntelCmds(app, cmd, &wsFlag, &repoFlag)
	cmd.AddCommand(&cobra.Command{Use: "impact", Short: "Change impact of the working tree vs. the default branch",
		RunE: run(func(ctx context.Context, p string) (string, error) { return app.intel().Impact(ctx, p, "", 2) })})
	cmd.AddCommand(&cobra.Command{Use: "architecture", Short: "Architecture overview",
		RunE: run(func(ctx context.Context, p string) (string, error) { return app.intel().Architecture(ctx, p) })})
	var kindFilter string
	links := &cobra.Command{Use: "links", Short: "Cross-service contract links (HTTP, topics, env) across the workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			eps, err := app.workspaceEndpoints(cmd.Context(), wsFlag)
			if err != nil {
				return err
			}
			ls := xservice.LinkAll(eps, xservice.LinkOptions{})
			if kindFilter != "" {
				var f []xservice.Link
				for _, l := range ls {
					if l.Kind == kindFilter {
						f = append(f, l)
					}
				}
				ls = f
			}
			if app.jsonOut {
				return app.printJSON(ls)
			}
			for _, l := range ls {
				app.printf("%s\n", l)
			}
			if u := xservice.Unresolved(eps); len(u) > 0 {
				app.printf("(%d endpoints with unresolved values are not linked; see `intel endpoints`)\n", len(u))
			}
			return nil
		}}
	links.Flags().StringVar(&kindFilter, "kind", "", "http | topic | topic_infra | env")
	endpoints := &cobra.Command{Use: "endpoints", Short: "Cross-service contract endpoints found by the analyzers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			eps, err := app.workspaceEndpoints(cmd.Context(), wsFlag)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(eps)
			}
			for _, e := range eps {
				if repoFlag != "" && e.Repo != repoFlag {
					continue
				}
				app.printf("%-15s %-34s %-9s %s %s\n", e.Kind, e.Key(), e.Confidence, e.Where(), e.Symbol)
			}
			return nil
		}}
	cmd.AddCommand(links, endpoints)
	return cmd
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func (a *App) workspaceEndpoints(ctx context.Context, wsFlag string) ([]xservice.Endpoint, error) {
	w, err := a.resolveWorkspace(ctx, wsFlag)
	if err != nil {
		return nil, err
	}
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	eps, err := xservice.LoadWorkspace(ctx, s.DB, w.ID)
	if err == nil && len(eps) == 0 {
		return nil, fmt.Errorf("no cross-service endpoints indexed for %s; run `index`", w.Name)
	}
	return eps, err
}
