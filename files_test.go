/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/tranquildata/config/mocks"
)

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), testConfigFile)
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func Test_flatJSONMissingFile(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	if provider, err := FlatJSONProvider(filepath.Join(t.TempDir(), "missing.json"), scheme); err == nil {
		t.Error("expected an error for a missing file")
	} else if provider != nil {
		t.Error("expected no provider for a missing file")
	}
}

func Test_flatJSONInvalidContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	for _, content := range []string{"", "{", "not json", `["root"]`, `{"root": "/opt/app"} trailing`, `{"root": "/opt/app"}{}`} {
		if provider, err := FlatJSONProvider(writeTestFile(t, content), scheme); err == nil {
			t.Errorf("expected an error for invalid content %q", content)
		} else if provider != nil {
			t.Errorf("expected no provider for invalid content %q", content)
		}
	}
}

func Test_flatJSONInvalidName(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("Root").Return(errors.New("invalid"))

	if provider, err := FlatJSONProvider(writeTestFile(t, `{"Root": "/opt/app"}`), scheme); err == nil {
		t.Error("expected an error for an invalid property name")
	} else if provider != nil {
		t.Error("expected no provider for an invalid property name")
	}
}

func Test_flatJSONEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)

	if provider, err := FlatJSONProvider(writeTestFile(t, `{}`), scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if _, present := provider.Value("root"); present {
		t.Error("expected no value from an empty configuration")
	}
}

func Test_flatJSONValues(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("root").Return(nil)
	scheme.EXPECT().Validate("port").Return(nil)
	scheme.EXPECT().Validate("enabled").Return(nil)
	scheme.EXPECT().Validate("ratio").Return(nil)
	scheme.EXPECT().Validate("empty").Return(nil)

	content := `{"root": "/opt/app", "port": 8080, "enabled": true, "ratio": 0.5, "empty": ""}`
	provider, err := FlatJSONProvider(writeTestFile(t, content), scheme)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{"root": "/opt/app", "port": "8080", "enabled": "true", "ratio": "0.5", "empty": ""}
	for name, expectedValue := range expected {
		if value, present := provider.Value(name); !present || value != expectedValue {
			t.Errorf("unexpected value for %s: %q (%t)", name, value, present)
		}
	}
	if _, present := provider.Value("missing"); present {
		t.Error("expected no value for missing property")
	}
}

func Test_flatJSONLargeInteger(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("maxConnections").Return(nil)

	if provider, err := FlatJSONProvider(writeTestFile(t, `{"maxConnections": 10000000}`), scheme); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if value, _ := provider.Value("maxConnections"); value != "10000000" {
		t.Errorf("unexpected value for large integer: %q", value)
	}
}

func Test_flatJSONObjectRejected(t *testing.T) {
	for _, content := range []string{`{"server": {}}`, `{"server": {"host": "localhost"}}`} {
		ctrl := gomock.NewController(t)
		scheme := mocks.NewMockNamingScheme(ctrl)
		scheme.EXPECT().Validate("server").Return(nil)

		if provider, err := FlatJSONProvider(writeTestFile(t, content), scheme); err == nil {
			t.Errorf("expected an error for object value %q", content)
		} else if provider != nil {
			t.Errorf("expected no provider for object value %q", content)
		}
	}
}

func Test_flatJSONNullRejected(t *testing.T) {
	for _, content := range []string{`{"hosts": null}`, `{"hosts": ["a", null]}`, `{"hosts": [null]}`} {
		ctrl := gomock.NewController(t)
		scheme := mocks.NewMockNamingScheme(ctrl)
		scheme.EXPECT().Validate("hosts").Return(nil)

		if provider, err := FlatJSONProvider(writeTestFile(t, content), scheme); err == nil {
			t.Errorf("expected an error for null value %q", content)
		} else if provider != nil {
			t.Errorf("expected no provider for null value %q", content)
		}
	}
}

func Test_flatJSONInvalidArrayRejected(t *testing.T) {
	for _, content := range []string{
		`{"hosts": [{"host": "localhost"}]}`,
		`{"hosts": ["a", ["b", "c"]]}`,
		`{"hosts": ["a` + SliceValueSeparator + `b", "c"]}`,
	} {
		ctrl := gomock.NewController(t)
		scheme := mocks.NewMockNamingScheme(ctrl)
		scheme.EXPECT().Validate("hosts").Return(nil)

		if provider, err := FlatJSONProvider(writeTestFile(t, content), scheme); err == nil {
			t.Errorf("expected an error for invalid array %q", content)
		} else if provider != nil {
			t.Errorf("expected no provider for invalid array %q", content)
		}
	}
}

func Test_flatJSONArrays(t *testing.T) {
	ctrl := gomock.NewController(t)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("names").Return(nil)
	scheme.EXPECT().Validate("counts").Return(nil)
	scheme.EXPECT().Validate("flags").Return(nil)
	scheme.EXPECT().Validate("single").Return(nil)
	scheme.EXPECT().Validate("empty").Return(nil)

	content := `{"names": ["a", "b", "c"], "counts": [1, 10000000, -3], "flags": [true, false], "single": ["only"], "empty": []}`
	provider, err := FlatJSONProvider(writeTestFile(t, content), scheme)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"names":  strings.Join([]string{"a", "b", "c"}, SliceValueSeparator),
		"counts": strings.Join([]string{"1", "10000000", "-3"}, SliceValueSeparator),
		"flags":  strings.Join([]string{"true", "false"}, SliceValueSeparator),
		"single": "only",
		"empty":  "",
	}
	for name, expectedValue := range expected {
		if value, present := provider.Value(name); !present || value != expectedValue {
			t.Errorf("unexpected value for %s: %q (%t)", name, value, present)
		}
	}
}
