/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"slices"
	"testing"
)

func Test_schemeConstructors(t *testing.T) {
	// every built-in scheme accepts single-word and single-letter, all-lowercase names
	for name, scheme := range map[string]NamingScheme{"camel": CamelNamingScheme(), "snake": SnakeNamingScheme(), "kebab": KebabNamingScheme()} {
		if scheme == nil {
			t.Errorf("expected a naming scheme for %s", name)
			continue
		}
		for _, propertyName := range []string{"a", "root"} {
			if err := scheme.Validate(propertyName); err != nil {
				t.Errorf("unexpected error for %q with scheme %s: %v", propertyName, name, err)
			} else if components := scheme.Components(propertyName); !slices.Equal(components, []string{propertyName}) {
				t.Errorf("unexpected components for %q with scheme %s: %v", propertyName, name, components)
			}
		}
	}
}

func Test_camelValidate(t *testing.T) {
	scheme := CamelNamingScheme()
	for _, name := range []string{"a", "root", "rootDirectory", "http2Port", "aBC"} {
		if err := scheme.Validate(name); err != nil {
			t.Errorf("unexpected error for valid camel name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "Root", "2root", "root_directory", "root-directory", "root directory", "root.directory"} {
		if err := scheme.Validate(name); err == nil {
			t.Errorf("expected an error for invalid camel name %q", name)
		}
	}
}

func Test_snakeValidate(t *testing.T) {
	scheme := SnakeNamingScheme()
	for _, name := range []string{"a", "a_b", "x_1_y", "root", "root_directory", "http_2_port", "ab_c"} {
		if err := scheme.Validate(name); err != nil {
			t.Errorf("unexpected error for valid snake name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "A", "2", "Root", "rootDirectory", "2root", "_root", "root_", "root__directory", "root-directory", "a_", "_a"} {
		if err := scheme.Validate(name); err == nil {
			t.Errorf("expected an error for invalid snake name %q", name)
		}
	}
}

func Test_kebabValidate(t *testing.T) {
	scheme := KebabNamingScheme()
	for _, name := range []string{"a", "A", "a-b", "x-1-y", "root", "root-directory", "http-2-port", "ab-c"} {
		if err := scheme.Validate(name); err != nil {
			t.Errorf("unexpected error for valid kebab name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "2", "2root", "-root", "root-", "root--directory", "root_directory", "a-", "-a"} {
		if err := scheme.Validate(name); err == nil {
			t.Errorf("expected an error for invalid kebab name %q", name)
		}
	}
}

func Test_camelComponents(t *testing.T) {
	scheme := CamelNamingScheme()
	if components := scheme.Components(""); len(components) != 0 {
		t.Errorf("unexpected components for empty name: %v", components)
	}
	if components := scheme.Components("root"); !slices.Equal(components, []string{"root"}) {
		t.Errorf("unexpected components for single-word name: %v", components)
	}
	if components := scheme.Components("rootDirectory"); !slices.Equal(components, []string{"root", "Directory"}) {
		t.Errorf("unexpected components for two-word name: %v", components)
	}
	if components := scheme.Components("maxHttp2Port"); !slices.Equal(components, []string{"max", "Http2", "Port"}) {
		t.Errorf("unexpected components for multi-word name: %v", components)
	}
	if components := scheme.Components("a"); !slices.Equal(components, []string{"a"}) {
		t.Errorf("unexpected components for one-character name: %v", components)
	}
}

func Test_camelComponentsCapitalRuns(t *testing.T) {
	scheme := CamelNamingScheme()
	cases := map[string][]string{
		"maxHTTPPort":  {"max", "HTTP", "Port"},
		"getURL":       {"get", "URL"},
		"useHTTPS":     {"use", "HTTPS"},
		"maxHTTP2Port": {"max", "HTTP2", "Port"},
		"aBC":          {"a", "BC"},
		"aB":           {"a", "B"},
		"aBc":          {"a", "Bc"},
		"xMLHttpUrl":   {"x", "ML", "Http", "Url"},
		"ioURLAPIKey":  {"io", "URLAPI", "Key"},
	}
	for name, expected := range cases {
		if components := scheme.Components(name); !slices.Equal(components, expected) {
			t.Errorf("unexpected components for %s: %v", name, components)
		}
	}
}

func Test_snakeComponents(t *testing.T) {
	scheme := SnakeNamingScheme()
	if components := scheme.Components("root"); !slices.Equal(components, []string{"root"}) {
		t.Errorf("unexpected components for single-word name: %v", components)
	}
	if components := scheme.Components("a_b"); !slices.Equal(components, []string{"a", "b"}) {
		t.Errorf("unexpected components for one-character words: %v", components)
	}
	if components := scheme.Components("root_directory"); !slices.Equal(components, []string{"root", "directory"}) {
		t.Errorf("unexpected components for two-word name: %v", components)
	}
	if components := scheme.Components("max_http_2_port"); !slices.Equal(components, []string{"max", "http", "2", "port"}) {
		t.Errorf("unexpected components for multi-word name: %v", components)
	}
}

func Test_kebabComponents(t *testing.T) {
	scheme := KebabNamingScheme()
	if components := scheme.Components("root"); !slices.Equal(components, []string{"root"}) {
		t.Errorf("unexpected components for single-word name: %v", components)
	}
	if components := scheme.Components("a-b"); !slices.Equal(components, []string{"a", "b"}) {
		t.Errorf("unexpected components for one-character words: %v", components)
	}
	if components := scheme.Components("root-directory"); !slices.Equal(components, []string{"root", "directory"}) {
		t.Errorf("unexpected components for two-word name: %v", components)
	}
	if components := scheme.Components("max-http-2-port"); !slices.Equal(components, []string{"max", "http", "2", "port"}) {
		t.Errorf("unexpected components for multi-word name: %v", components)
	}
}

func Test_compose(t *testing.T) {
	cases := map[string]struct {
		scheme     NamingScheme
		components []string
		expected   string
	}{
		"camel":         {CamelNamingScheme(), []string{"index", "postgres", "Port"}, "indexPostgresPort"},
		"camel-lower":   {CamelNamingScheme(), []string{"index", "postgres", "port"}, "indexPostgresPort"},
		"camel-acronym": {CamelNamingScheme(), []string{"max", "HTTP", "Connections"}, "maxHTTPConnections"},
		"camel-digits":  {CamelNamingScheme(), []string{"max", "http2", "port"}, "maxHttp2Port"},
		"camel-unicode": {CamelNamingScheme(), []string{"max", "élan"}, "maxÉlan"},
		"snake":         {SnakeNamingScheme(), []string{"index", "postgres", "port"}, "index_postgres_port"},
		"kebab":         {KebabNamingScheme(), []string{"index", "Postgres", "port"}, "index-Postgres-port"},
	}
	for name, testCase := range cases {
		if composed := testCase.scheme.Compose(testCase.components); composed != testCase.expected {
			t.Errorf("unexpected composed name for %s: %q", name, composed)
		}
	}

	// composing no words, or a single word, is well-defined for every scheme
	for name, scheme := range map[string]NamingScheme{"camel": CamelNamingScheme(), "snake": SnakeNamingScheme(), "kebab": KebabNamingScheme()} {
		if composed := scheme.Compose(nil); composed != "" {
			t.Errorf("unexpected composed name for no words with scheme %s: %q", name, composed)
		}
		if composed := scheme.Compose([]string{"root"}); composed != "root" {
			t.Errorf("unexpected composed name for one word with scheme %s: %q", name, composed)
		}
		if composed := scheme.Compose([]string{"root", ""}); name == "camel" && composed != "root" {
			t.Errorf("unexpected composed name with an empty word for scheme %s: %q", name, composed)
		}
	}
}

func Test_composeInvertsComponents(t *testing.T) {
	// composing the components of a valid name returns that name
	cases := map[string][]string{
		"camel": {"a", "root", "rootDirectory", "maxHTTPPort", "maxHttp2Port", "aBC", "parseURLs"},
		"snake": {"a", "root", "root_directory", "http_2_port"},
		"kebab": {"a", "A", "root", "root-directory", "Root-Dir", "http-2-port"},
	}
	schemes := map[string]NamingScheme{"camel": CamelNamingScheme(), "snake": SnakeNamingScheme(), "kebab": KebabNamingScheme()}
	for name, propertyNames := range cases {
		scheme := schemes[name]
		for _, propertyName := range propertyNames {
			if err := scheme.Validate(propertyName); err != nil {
				t.Errorf("invalid test name %q for scheme %s: %v", propertyName, name, err)
			} else if composed := scheme.Compose(scheme.Components(propertyName)); composed != propertyName {
				t.Errorf("unexpected round-trip for %q with scheme %s: %q", propertyName, name, composed)
			}
		}
	}
}
