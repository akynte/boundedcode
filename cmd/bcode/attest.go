package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/attest"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/task"
)

// newTaskAttestCmd prints a task's verification records from the evidence
// chain and verifies the whole chain they sit in.
func newTaskAttestCmd() *cobra.Command {
	var asJSON bool
	var pubPath, head string
	cmd := &cobra.Command{
		Use:   "attest <task-id>",
		Short: "Show a task's signed verification records and verify the evidence chain",
		Long: "attest lists every verification run recorded for a task: the base commit, the\n" +
			"SHA-256 of the exact patch verified, the oracle digest, and each check's\n" +
			"verdict. It then verifies the whole evidence chain: every hash, every link,\n" +
			"and every signature against the verifier's public key.\n\n" +
			"--pub verifies against an exported public key instead of this data\n" +
			"directory's, which is how someone who does not trust this machine checks it.\n" +
			"--head checks that the chain still reaches a hash recorded elsewhere, such as\n" +
			"the Evidence-Head trailer of the task's commit; without it a chain with its\n" +
			"newest records removed still verifies.\n\n" +
			"Exit status is 1 when the chain is broken or the head is missing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			if pubPath == "" {
				pubPath = attest.PublicPath(root.Layout().KeysDir())
			}
			pub, err := attest.LoadPublic(pubPath)
			if err != nil {
				return fmt.Errorf("the verifier public key: %w", err)
			}
			keys := map[string]ed25519.PublicKey{attest.KeyID(pub): pub}

			l := ledger.New(st)
			all, err := l.Chain(ctx, "")
			if err != nil {
				return err
			}
			report := ledger.VerifyRecords(all, keys)
			var mine []ledger.ChainRecord
			headFound := head == ""
			for _, rec := range all {
				if rec.TaskID == args[0] {
					mine = append(mine, rec)
				}
				headFound = headFound || rec.Hash == head
			}
			ok := report.Intact() && headFound

			if asJSON {
				if err := emitJSON(map[string]any{
					"task_id": args[0], "records": mine, "chain": report,
					"key_id": attest.KeyID(pub), "head_found": headFound, "ok": ok,
				}); err != nil {
					return err
				}
			} else {
				printAttestation(cmd, args[0], mine, report, attest.KeyID(pub), head, headFound)
			}
			if !ok {
				closeRoot(cmd, root) // os.Exit skips the deferred close
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	cmd.Flags().StringVar(&pubPath, "pub", "", "verifier public key file (default: this data directory's)")
	cmd.Flags().StringVar(&head, "head", "", "a chain hash recorded elsewhere that the chain must still contain")
	return cmd
}

func printAttestation(cmd *cobra.Command, taskID string, mine []ledger.ChainRecord,
	report ledger.ChainReport, keyID, head string, headFound bool) {

	w := cmd.OutOrStdout()
	if len(mine) == 0 {
		fmt.Fprintf(w, "no verification records for %s\n", taskID)
	}
	for _, rec := range mine {
		var v task.VerificationRecord
		_ = json.Unmarshal(rec.Payload, &v)
		fmt.Fprintf(w, "#%d  %s  %s\n", rec.Seq, rec.CreatedAt.Format("2006-01-02 15:04:05Z"), rec.Hash)
		fmt.Fprintf(w, "  base %.12s  patch %.12s  candidate %.12s  key %s\n",
			v.Base, v.PatchSHA256, v.Candidate, orDash(rec.KeyID))
		if v.OracleDigest != "" {
			fmt.Fprintf(w, "  oracle %.12s  hidden checks %v\n", v.OracleDigest, v.HiddenChecks)
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, res := range v.Results {
			fmt.Fprintf(tw, "  \t%s\t%s\t%s\n", res.Recipe, res.Status, res.Headline)
		}
		_ = tw.Flush()
	}

	fmt.Fprintln(w)
	switch {
	case report.Intact():
		fmt.Fprintf(w, "chain intact: %d record(s), every signature verified against key %s\n", report.Records, keyID)
	default:
		fmt.Fprintf(w, "CHAIN BROKEN: %d record(s)\n", report.Records)
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(w, "  #%d: %s\n", issue.Seq, issue.Problem)
	}
	fmt.Fprintf(w, "head %s\n", orDash(report.Head))
	if head != "" {
		if headFound {
			fmt.Fprintf(w, "the chain contains %s\n", head)
		} else {
			fmt.Fprintf(w, "HEAD MISSING: the chain does not contain %s; records were removed or rewritten\n", head)
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
