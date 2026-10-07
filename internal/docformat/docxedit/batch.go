package docxedit

import "fmt"

// Batch collects edits against one snapshot of the document and applies
// them with a single rewrite and re-tokenization (see Document.Batch).
type Batch struct {
	d     *Document
	edits []batchEdit
}

type batchEdit struct {
	what string // e.g. "ReplaceText on paragraph 3"
	sp   []splice
}

// Batch runs fn, collecting the edits it requests through b, then applies
// them all at once: splices sorted by descending offset, one reload. This is
// what to use for hundreds of edits (each Document method re-tokenizes
// document.xml).
//
// Every edit is planned against the document as it was when Batch started,
// so two edits must not touch the same markup. Edits on different
// paragraphs / sections never do; on one paragraph, SetParaProps (w:pPr)
// combines with SetRunProps or a text replacement (runs), but two run-level
// edits (ReplaceText, ReplaceSubstring, SetRunProps) on the same paragraph,
// two SetParaProps on it, or SetParaProps plus SetSectionProps on the
// section break it carries conflict. A conflicting call returns an error
// naming both edits; when fn returns an error (that one or its own), nothing
// is applied and the document is unchanged.
func (d *Document) Batch(fn func(b *Batch) error) error {
	b := &Batch{d: d}
	if err := fn(b); err != nil {
		return err
	}
	var all []splice
	for _, e := range b.edits {
		all = append(all, e.sp...)
	}
	return d.apply(all)
}

// add records the splices of one edit after checking them against every
// earlier edit of the batch.
func (b *Batch) add(what string, sp []splice, err error) error {
	if err != nil {
		return err
	}
	for _, e := range b.edits {
		for _, x := range e.sp {
			for _, y := range sp {
				if overlaps(x, y) {
					return fmt.Errorf("docxedit: batch: %s overlaps %s; apply them in separate batches", what, e.what)
				}
			}
		}
	}
	if len(sp) > 0 {
		b.edits = append(b.edits, batchEdit{what, sp})
	}
	return nil
}

// ReplaceText is Document.ReplaceText, deferred to the end of the batch.
func (b *Batch) ReplaceText(index int, newText string, a Author) error {
	sp, err := b.d.planReplaceText(index, newText, a)
	return b.add(fmt.Sprintf("ReplaceText on paragraph %d", index), sp, err)
}

// ReplaceSubstring is Document.ReplaceSubstring, deferred to the end of the batch.
func (b *Batch) ReplaceSubstring(index int, old, new string, a Author) error {
	sp, err := b.d.planReplaceSubstring(index, old, new, a)
	return b.add(fmt.Sprintf("ReplaceSubstring on paragraph %d", index), sp, err)
}

// SetRunProps is Document.SetRunProps, deferred to the end of the batch.
func (b *Batch) SetRunProps(index int, p RunProps, a Author) error {
	sp, err := b.d.planSetRunProps(index, p, a)
	return b.add(fmt.Sprintf("SetRunProps on paragraph %d", index), sp, err)
}

// SetParaProps is Document.SetParaProps, deferred to the end of the batch.
func (b *Batch) SetParaProps(index int, p ParaProps, a Author) error {
	sp, err := b.d.planSetParaProps(index, p, a)
	return b.add(fmt.Sprintf("SetParaProps on paragraph %d", index), sp, err)
}

// SetSectionProps is Document.SetSectionProps, deferred to the end of the batch.
func (b *Batch) SetSectionProps(sectIndex int, p SectionProps, a Author) error {
	sp, err := b.d.planSetSectionProps(sectIndex, p, a)
	return b.add(fmt.Sprintf("SetSectionProps on section %d", sectIndex), sp, err)
}
