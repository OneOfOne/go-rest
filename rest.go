package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

type Reply struct {
	Err    error
	Status int
	Header http.Header
	Value  []byte
}

func (r *Reply) Unmarshal(v interface{}) error {
	return json.Unmarshal(r.Value, v)
}

type Client struct {
	HTTPClient  http.Client
	BasaeURL    string
	ContentType string
}

func New(baseURL string) *Client {
	c := &Client{
		BasaeURL:    baseURL,
		ContentType: "application/json",
	}
	c.Reset()
	return c
}

var re = regexp.MustCompile(`/+`)

func (c *Client) GetFullURL(subPath string) (*url.URL, error) {
	if strings.HasPrefix(subPath, "http:") || strings.HasPrefix(subPath, "https:") {
		return url.Parse(subPath)
	}

	u, err := url.Parse(c.BasaeURL + "/" + subPath)
	if u != nil {
		u.Path = re.ReplaceAllString(u.Path, "/")
	}
	return u, err

}
func (c *Client) Do(method, path string, body io.Reader, out interface{}) (r Reply) {
	var u *url.URL
	if u, r.Err = c.GetFullURL(path); r.Err != nil {
		return
	}
	var req *http.Request
	if req, r.Err = http.NewRequest(method, u.String(), body); r.Err != nil {
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", c.ContentType)
	}
	var resp *http.Response
	if resp, r.Err = c.HTTPClient.Do(req); r.Err != nil {
		return
	}
	defer resp.Body.Close()
	r.Status, r.Header = resp.StatusCode, resp.Header
	if r.Value, r.Err = ioutil.ReadAll(resp.Body); r.Err != nil {
		return
	}
	if out != nil {
		r.Err = r.Unmarshal(out)
	}
	return
}

func (c *Client) Reset() {
	c.HTTPClient.Jar, _ = cookiejar.New(nil)
}
