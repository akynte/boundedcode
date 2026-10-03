package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
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
				repos, err := ws.Repos(cmd.Context(), w.ID)
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
			rec, _ := app.Recorder(ctx)
			intel := app.intel()
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
				rec.Emit(ctx, "", "repointel.indexed", map[string]any{"repo": r.Name, "project": res.Project, "nodes": res.Nodes, "edges": res.Edges, "seconds": time.Since(t0).Seconds()})
				app.printf("%-24s project=%s nodes=%d edges=%d %.1fs\n", r.Name, res.Project, res.Nodes, res.Edges, time.Since(t0).Seconds())
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
	cmd.AddCommand(&cobra.Command{Use: "impact", Short: "Change impact of the working tree vs. the default branch",
		RunE: run(func(ctx context.Context, p string) (string, error) { return app.intel().Impact(ctx, p, "", 2) })})
	cmd.AddCommand(&cobra.Command{Use: "architecture", Short: "Architecture overview",
		RunE: run(func(ctx context.Context, p string) (string, error) { return app.intel().Architecture(ctx, p) })})
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
