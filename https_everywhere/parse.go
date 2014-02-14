package https_everywhere

import (
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/glenn-brown/golang-pkg-pcre/src/pkg/pcre"
)

type Rule struct {
	From_ string `xml:"from,attr"`
	From  pcre.Regexp
	To    string `xml:"to,attr"`
}

func (r *Rule) Initialize() error {
	var err *pcre.CompileError
	r.From, err = pcre.Compile(r.From_, 0)
	if err != nil {
		return errors.New(err.String())
	}

	return nil
}

func (r *Rule) Apply(url string) (string, bool) {
	matcher := r.From.MatcherString(url, 0)
	if !matcher.Matches() {
		return url, false
	}

	newUrl, groups := url, matcher.Groups()
	newUrl = strings.Replace(newUrl, matcher.GroupString(0), r.To, 1)
	for group := 1; group <= groups; group++ {
		replaceWith := matcher.GroupString(group)
		newUrl = strings.Replace(newUrl, "$"+strconv.FormatInt(int64(group), 10), replaceWith, -1)
	}

	return newUrl, true
}

type Exclusion struct {
	Pattern_ string `xml:"pattern,attr"`
	Pattern  pcre.Regexp
}

func (e *Exclusion) Initialize() error {
	var err *pcre.CompileError
	e.Pattern, err = pcre.Compile(e.Pattern_, 0)
	if err != nil {
		return errors.New(err.String())
	}

	return nil
}

func (e *Exclusion) Match(url string) bool {
	matcher := e.Pattern.MatcherString(url, 0)
	return matcher.Matches()
}

type Target struct {
	Host string `xml:"host,attr"`
}

func (t *Target) Initialize() error {
	t.Host = strings.ToLower(t.Host)
	return nil
}

func (t *Target) Match(host string) bool {
	if strings.HasPrefix(t.Host, "*.") {
		return strings.HasSuffix(host, t.Host[1:])
	}

	return t.Host == host
}

type RuleFile struct {
	XMLName    *xml.Name    `xml:"ruleset"`
	DefaultOff string       `xml:"default_off,attr"`
	Targets    []*Target    `xml:"target"`
	Exclusions []*Exclusion `xml:"exclusion"`
	Rules      []*Rule      `xml:"rule"`
}

func (r *RuleFile) Initialize() error {
	for _, target := range r.Targets {
		if err := target.Initialize(); err != nil {
			return err
		}
	}
	for _, exclusion := range r.Exclusions {
		if err := exclusion.Initialize(); err != nil {
			return err
		}
	}
	for _, rule := range r.Rules {
		if err := rule.Initialize(); err != nil {
			return err
		}
	}

	return nil
}

func (r *RuleFile) Apply(original string, host string) (bool, string, error) {
	if len(host) <= 0 {
		parsedOriginal, err := url.Parse(original)
		if err != nil {
			return false, original, err
		}
		host = strings.ToLower(parsedOriginal.Host)
	}

	matchesTargets := false
	for _, target := range r.Targets {
		if target.Match(host) {
			matchesTargets = true
			break
		}
	}
	if !matchesTargets {
		return false, original, nil
	}

	for _, exclusion := range r.Exclusions {
		if exclusion.Match(original) {
			return false, original, nil
		}
	}

	for _, rule := range r.Rules {
		if newUrl, applied := rule.Apply(original); applied {
			return applied, newUrl, nil
		}
	}

	return false, original, nil
}

func ParseRuleFile(r io.Reader) (*RuleFile, error) {
	decoder := xml.NewDecoder(r)

	ruleFile := new(RuleFile)
	if err := decoder.Decode(&ruleFile); err != nil {
		return nil, err
	}
	if err := ruleFile.Initialize(); err != nil {
		return nil, err
	}

	return ruleFile, nil
}

type RuleSet []*RuleFile

func (r RuleSet) Apply(original string, host string) (bool, string, error) {
	if len(host) <= 0 {
		parsedOriginal, err := url.Parse(original)
		if err != nil {
			return false, original, err
		}
		host = strings.ToLower(parsedOriginal.Host)
	}

	for _, rule := range r {
		if applied, newUrl, err := rule.Apply(original, host); applied || err != nil {
			return applied, newUrl, err
		}
	}

	return false, original, nil
}

func ParseDirectory(dir string) (RuleSet, error) {
	rs := make(RuleSet, 0)

	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		} else if info.IsDir() {
			return nil
		} else if filepath.Ext(path) != ".xml" {
			return nil
		}

		file, err := os.OpenFile(path, os.O_RDONLY, 0644)
		if err != nil {
			return err
		}

		parsed, err := ParseRuleFile(file)
		if err != nil {
			return err
		} else if len(parsed.DefaultOff) > 0 {
			return nil
		}

		rs = append(rs, parsed)
		return nil
	}); err != nil {
		return nil, err
	}

	return rs, nil
}
