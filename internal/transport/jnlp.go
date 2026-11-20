package transport

import (
	"encoding/xml"
	"fmt"
)

// JNLP is what OldIPMI reads of a Java Web Start file: the main class and its
// arguments, which tell how to reach the console. It never runs or fetches the
// resources a JNLP names.
type JNLP struct {
	Main string
	Args []string
}

// ParseJNLP reads a JNLP file of at most 1 MiB.
func ParseJNLP(b []byte) (JNLP, error) {
	var doc struct {
		XMLName xml.Name `xml:"jnlp"`
		App     struct {
			Main string   `xml:"main-class,attr"`
			Args []string `xml:"argument"`
		} `xml:"application-desc"`
	}
	if len(b) > 1<<20 {
		return JNLP{}, fmt.Errorf("JNLP exceeds size limit")
	}
	if e := xml.Unmarshal(b, &doc); e != nil {
		return JNLP{}, fmt.Errorf("invalid or incomplete JNLP XML")
	}
	return JNLP{Main: doc.App.Main, Args: doc.App.Args}, nil
}
