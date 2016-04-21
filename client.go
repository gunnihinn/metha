package perimorph

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sethgrid/pester"
)

const (
	DefaultTimeout    = 5 * time.Minute
	DefaultMaxRetries = 8
)

// defaultDoer is a resilient http client with retries and longer timeout.
var defaultDoer = func() Doer {
	c := pester.New()
	c.Timeout = DefaultTimeout
	c.MaxRetries = DefaultMaxRetries
	c.Backoff = pester.ExponentialBackoff
	return c
}()

var (
	DefaultClient = Client{Doer: defaultDoer}
	// Example for broken XML: http://eprints.vu.edu.au/perl/oai2. Add more
	// weird things to be cleaned before XML parsing here. Another faulty:
	// http://digitalcommons.gardner-webb.edu/do/oai/?from=2016-02-29&metadataPr
	// efix=oai_dc&until=2016-03-31&verb=ListRecords. Replace control chars
	// outside XML char range.
	ControlCharReplacer = strings.NewReplacer(
		"\u0001", "", "\u0002", "", "\u0003", "",
		"\u0004", "", "\u0005", "", "\u0006", "",
		"\u0007", "", "\u0008", "", "\u0009", "",
		"\u000B", "", "\u000C", "", "\u000E", "",
		"\u000F", "", "\u0010", "", "\u0011", "",
		"\u0012", "", "\u0013", "", "\u0014", "",
		"\u0015", "", "\u0016", "", "\u0017", "",
		"\u0018", "", "\u0019", "", "\u001A", "",
		"\u001B", "", "\u001C", "", "\u001D", "",
		"\u001E", "", "\u001F", "")
)

// Doer is a minimal HTTP interface.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// A client that can execute requests.
type Client struct {
	Doer Doer
}

// Do is a shortcut for DefaultClient.Do.
func Do(r *Request) (*Response, error) {
	return DefaultClient.Do(r)
}

// anyReadCloser detects compressed content and decompresses it on the fly.
func maybeCompressed(r io.Reader) (io.ReadCloser, error) {
	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if gr, err := gzip.NewReader(bytes.NewReader(buf)); err == nil {
		log.Println("decompress-on-the-fly")
		return gr, nil
	}
	return ioutil.NopCloser(bytes.NewReader(buf)), nil
}

// Do executes a single OAIRequest. ResumptionToken handling must happen in the
// caller. Only Identify and GetRecord requests will return a complete response.
func (c *Client) Do(r *Request) (*Response, error) {
	link, err := r.URL()
	if err != nil {
		return nil, err
	}
	log.Println(link)

	req, err := http.NewRequest("GET", link.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.Doer.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("error: server returned %s for %s", http.StatusText(resp.StatusCode), link)
	}
	defer resp.Body.Close()

	var reader io.ReadCloser = resp.Body

	// detect compressed response
	reader, err = maybeCompressed(reader)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	if r.CleanBeforeDecode {
		// remove some chars, that the XML decoder will complain about
		b, err := ioutil.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		reader = ioutil.NopCloser(strings.NewReader(ControlCharReplacer.Replace(string(b))))
	}

	dec := xml.NewDecoder(reader)

	var response Response
	if err := dec.Decode(&response); err != nil {
		return nil, err
	}
	return &response, nil
}