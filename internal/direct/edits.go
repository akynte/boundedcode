package direct

import (
	"errors"
	"fmt"

	"github.com/akynte/boundedcode/internal/capability"
	"github.com/akynte/boundedcode/internal/fileedit"
	"github.com/akynte/boundedcode/internal/retrieve"
)

// The file tools (write, edit, multi_edit) apply their changes through
// internal/fileedit: each file is replaced atomically, a multi_edit
// changes all its files or none, and a file that changed since the model
// last saw it is detected. A whole-file write over such a file is refused
// (it was written from the old content); an edit whose text still matches
// goes ahead and says so.

// seenBlob is the blob of a root-relative path as the model last saw or
// wrote it in the history ("" when it has not, or no longer has it).
func (r *Runner) seenBlob(rel string) string {
	if r.Session == nil {
		return ""
	}
	for i := len(r.Session.Messages) - 1; i >= 0; i-- {
		for _, s := range r.Session.Messages[i].Stamps {
			if s.Path == rel {
				return s.Blob
			}
		}
	}
	return ""
}

// changedSinceSeen is a note for an edit to a file whose content (before)
// is not what the model last saw.
func (r *Runner) changedSinceSeen(abs string, before []byte) string {
	rel := r.rootRel(abs)
	seen := r.seenBlob(rel)
	if seen == "" || before == nil || retrieve.BlobID(before) == seen {
		return ""
	}
	return "\nnote: " + rel + " changed since you last read it (not by your edits): the text you replaced matched, but read the file again before relying on the rest of it."
}

// editError is the model's message for a failed write or edit, worded as
// before the engine was shared.
func (r *Runner) editError(err error, rel string) error {
	var ce *fileedit.ConflictError
	var ee *fileedit.EditError
	var ae *fileedit.AmbiguousError
	switch {
	case errors.As(err, &ce):
		if ce.Got == "" {
			return &capability.Error{Code: capability.CodeConflict, Message: rel + " was deleted since you last read it; look again before writing it"}
		}
		return &capability.Error{Code: capability.CodeConflict, Message: rel + " changed since you last read it (by a command, or outside this session); read it again, then write it, or change it with edit"}
	case errors.As(err, &ae) && ae.Count > 0:
		return fmt.Errorf("old_string occurs %d times in %s; include more context or set replace_all", ae.Count, rel)
	case errors.As(err, &ee):
		return fmt.Errorf("%w in %s; read the file and copy the text exactly (indentation is matched loosely, other characters must be identical)", ee.Err, rel)
	}
	return err
}

// multiEditError is editError for multi_edit, which names the edit
// (multi_edit makes one change per edit).
func (r *Runner) multiEditError(err error) error {
	var ce *fileedit.ConflictError
	var ee *fileedit.EditError
	var ae *fileedit.AmbiguousError
	switch {
	case errors.As(err, &ce):
		return r.editError(err, r.rootRel(ce.Path)+" (nothing was written)")
	case errors.As(err, &ee):
		rel := r.rootRel(ee.Path)
		n := ee.Change + 1 // one change per edit
		if errors.As(err, &ae) && ae.Count > 0 {
			return fmt.Errorf("edit %d: old_string occurs %d times in %s (nothing was written)", n, ae.Count, rel)
		}
		return fmt.Errorf("edit %d: %w in %s (nothing was written)", n, ee.Err, rel)
	}
	return err
}
