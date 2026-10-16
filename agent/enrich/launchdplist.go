package enrich

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
)

// The bounds a plist read keeps to. A plist is the user's file, read by a root daemon, so everything taken from it is capped.
const (
	maxPlistBytes    = 64 << 10
	maxJobFieldBytes = 1 << 10
	maxJobArgs       = 64
	maxJobArgBytes   = 4 << 10
)

// errNotJob is a plist that is not an XML property list whose top level is a dictionary.
var errNotJob = errors.New("not a launchd job plist")

// parseLaunchdPlist reads the job an XML property list defines: its Label, Program and ProgramArguments, each capped. The rest of
// the dictionary is skipped. A file that is not an XML plist with a dictionary at its top, or defines none of the three, is no job.
func parseLaunchdPlist(data []byte) (LaunchdJob, bool) {
	d := xml.NewDecoder(bytes.NewReader(data))
	// A plist's DOCTYPE names Apple's DTD; nothing is fetched, and entities beyond XML's own are refused by the decoder.
	d.Strict = true
	if err := enterTopDict(d); err != nil {
		return LaunchdJob{}, false
	}
	var job LaunchdJob
	readers := map[string]func() error{
		"Label":   func() (err error) { job.Label, err = readString(d, &job.Truncated, maxJobFieldBytes); return err },
		"Program": func() (err error) { job.Program, err = readString(d, &job.Truncated, maxJobFieldBytes); return err },
		"ProgramArguments": func() (err error) {
			job.ProgramArguments, err = readStrings(d, &job.Truncated)
			return err
		},
	}
	for {
		key, err := nextKey(d)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return LaunchdJob{}, false
		}
		read, kept := readers[key]
		if !kept {
			read = func() error { return skipValue(d) }
		}
		if read() != nil {
			return LaunchdJob{}, false
		}
	}
	return job, job.Label != "" || job.Program != "" || len(job.ProgramArguments) > 0
}

// enterTopDict advances past <plist> to the start of its top-level <dict>.
func enterTopDict(d *xml.Decoder) error {
	seenPlist := false
	for {
		tok, err := d.Token()
		if err != nil {
			return errNotJob
		}
		if start, ok := tok.(xml.StartElement); ok {
			switch {
			case !seenPlist && start.Name.Local == "plist":
				seenPlist = true
			case seenPlist && start.Name.Local == "dict":
				return nil
			default:
				return errNotJob
			}
		}
	}
}

// nextKey reads the dictionary's next <key>, or io.EOF at its end.
func nextKey(d *xml.Decoder) (string, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return "", errNotJob
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return "", io.EOF
		case xml.StartElement:
			if t.Name.Local != "key" {
				return "", errNotJob
			}
			var key string
			if err := d.DecodeElement(&key, &t); err != nil {
				return "", errNotJob
			}
			return key, nil
		}
	}
}

// nextStart returns the next element's start, skipping character data between elements.
func nextStart(d *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return xml.StartElement{}, errNotJob
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return t, nil
		case xml.EndElement:
			return xml.StartElement{}, errNotJob
		}
	}
}

// readString reads a <string> value, cut to limit bytes on a rune boundary, setting truncated when it was cut. Any other value type
// is skipped and read as empty, which a job's string keys never carry.
func readString(d *xml.Decoder, truncated *bool, limit int) (string, error) {
	start, err := nextStart(d)
	if err != nil {
		return "", err
	}
	if start.Name.Local != "string" {
		return "", d.Skip()
	}
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return "", errNotJob
	}
	return capString(s, truncated, limit), nil
}

// readStrings reads an <array> of <string> values, at most maxJobArgs of them, each capped; any other element in it is skipped.
func readStrings(d *xml.Decoder, truncated *bool) ([]string, error) {
	start, err := nextStart(d)
	if err != nil {
		return nil, err
	}
	if start.Name.Local != "array" {
		return nil, d.Skip()
	}
	var out []string
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, errNotJob
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return out, nil
		case xml.StartElement:
			if out, err = arrayElement(d, t, out, truncated); err != nil {
				return nil, err
			}
		}
	}
}

// arrayElement reads one element of a string array into out: a <string> while there is room, cut to its bound, and anything else,
// or a string past the count, skipped.
func arrayElement(d *xml.Decoder, t xml.StartElement, out []string, truncated *bool) ([]string, error) {
	if t.Name.Local != "string" || len(out) == maxJobArgs {
		*truncated = *truncated || len(out) == maxJobArgs
		if err := d.Skip(); err != nil {
			return nil, errNotJob
		}
		return out, nil
	}
	var s string
	if err := d.DecodeElement(&s, &t); err != nil {
		return nil, errNotJob
	}
	return append(out, capString(s, truncated, maxJobArgBytes)), nil
}

// skipValue skips the dictionary value that follows a key the read does not keep.
func skipValue(d *xml.Decoder) error {
	if _, err := nextStart(d); err != nil {
		return err
	}
	return d.Skip()
}

// capString cuts s to limit bytes on a rune boundary.
func capString(s string, truncated *bool, limit int) string {
	if len(s) <= limit {
		return s
	}
	*truncated = true
	cut := limit
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
}
