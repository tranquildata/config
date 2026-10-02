/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/tranquildata/config/mocks"
)

const testAppName = "configtest"

const testConfigFile = "config.json"

const testResourceDir = "etc"

type serviceTestConfig struct {
	Name string `config:"name,default"`
	Port int    `config:"port,80"`
	Host string `config:"host"`
}

func writeConfigFile(t *testing.T, rootDir, resourceDir, configFile, content string) {
	t.Helper()
	directory := filepath.Join(rootDir, resourceDir)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, configFile), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeResourceConfig(t *testing.T, rootDir, content string) {
	t.Helper()
	writeConfigFile(t, rootDir, testResourceDir, testConfigFile, content)
}

func loadHost(t *testing.T, service Service) string {
	t.Helper()
	config := &innerConfig{}
	if err := service.LoadConfig(config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return config.Host
}

func Test_setupNilScheme(t *testing.T) {
	// a missing scheme should be reported as an error, not cause a panic later in setup
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic for a nil scheme: %v", r)
		}
	}()

	if service, err := Setup(testAppName, []string{"root=" + t.TempDir()}, NoConfigFile, nil); err == nil {
		t.Error("expected an error for a nil scheme")
	} else if service != nil {
		t.Error("expected no service for a nil scheme")
	}
}

func Test_setupMisformedElement(t *testing.T) {
	if service, err := Setup(testAppName, []string{"root"}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a misformed element")
	} else if service != nil {
		t.Error("expected no service for a misformed element")
	}
}

func Test_setupInvalidName(t *testing.T) {
	if service, err := Setup(testAppName, []string{"root_directory=" + t.TempDir()}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an invalid property name")
	} else if service != nil {
		t.Error("expected no service for an invalid property name")
	}
}

func Test_setupInvalidAppName(t *testing.T) {
	for _, name := range []string{"my-app", "my app", "1app"} {
		if service, err := Setup(name, []string{"root=" + t.TempDir()}, NoConfigFile, CamelNamingScheme()); err == nil {
			t.Errorf("expected an error for application name %q", name)
		} else if service != nil {
			t.Errorf("expected no service for application name %q", name)
		}
	}
}

func Test_setupRepeatedElement(t *testing.T) {
	if service, err := Setup(testAppName, []string{"root=" + t.TempDir(), "root=" + t.TempDir()}, NoConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a repeated element")
	} else if service != nil {
		t.Error("expected no service for a repeated element")
	}
}

func Test_setupRootMissing(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing")

	if service, err := Setup(testAppName, []string{"root=" + missingDir}, NoConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a missing root directory")
	} else if !strings.Contains(err.Error(), missingDir) {
		t.Errorf("expected the root directory in the error: %v", err)
	} else if service != nil {
		t.Error("expected no service for a missing root directory")
	}
}

func Test_setupRootNotDirectory(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(rootFile, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	if service, err := Setup(testAppName, []string{"root=" + rootFile}, NoConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a root that isn't a directory")
	} else if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error: %v", err)
	} else if service != nil {
		t.Error("expected no service for a root that isn't a directory")
	}
}

func Test_setupCamelAcronymEnvironment(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("CONFIGTEST_MAX_HTTP_PORT", "8080")

	service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := &struct {
		MaxHTTPPort int `config:"maxHTTPPort"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.MaxHTTPPort != 8080 {
		t.Errorf("unexpected port: %d", config.MaxHTTPPort)
	}
}

func Test_setupRootFromList(t *testing.T) {
	rootDir := t.TempDir()

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != rootDir {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	} else if service.ResourceDir() != filepath.Join(rootDir, testResourceDir) {
		t.Errorf("unexpected resource directory: %s", service.ResourceDir())
	} else if resolved := service.AllProperties(); resolved["root"] != rootDir {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_setupRootCleaned(t *testing.T) {
	rootDir := t.TempDir()

	if service, err := Setup(testAppName, []string{"root=" + rootDir + "/./sub/../"}, NoConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != rootDir {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	}
}

func Test_setupRootFromEnvironment(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("CONFIGTEST_ROOT", rootDir)

	if service, err := Setup(testAppName, nil, NoConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != rootDir {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	}
}

func Test_setupListOverridesEnvironment(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("CONFIGTEST_ROOT", t.TempDir())

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != rootDir {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	}
}

func Test_setupRootFromBinary(t *testing.T) {
	binaryPath, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Dir(filepath.Dir(binaryPath))

	if service, err := Setup(testAppName, nil, NoConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != expected {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	} else if service.ResourceDir() != filepath.Join(expected, testResourceDir) {
		t.Errorf("unexpected resource directory: %s", service.ResourceDir())
	} else if resolved := service.AllProperties(); len(resolved) != 1 || resolved["resource"] != testResourceDir {
		t.Errorf("expected only the default resource property when root is unset: %v", resolved)
	}
}

func Test_setupConfigFilePriority(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"name": "file", "port": 1000, "host": "filehost"}`)
	t.Setenv("CONFIGTEST_PORT", "2000")
	t.Setenv("CONFIGTEST_HOST", "envhost")

	service, err := Setup(testAppName, []string{"root=" + rootDir, "host=listhost"}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := &serviceTestConfig{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Name != "file" {
		t.Errorf("expected name from file: %q", config.Name)
	} else if config.Port != 2000 {
		t.Errorf("expected port from environment: %d", config.Port)
	} else if config.Host != "listhost" {
		t.Errorf("expected host from list: %q", config.Host)
	}

	resolved := service.AllProperties()
	expected := map[string]string{"root": rootDir, "resource": testResourceDir, "name": "file", "port": "2000", "host": "listhost"}
	if len(resolved) != len(expected) {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
	for name, value := range expected {
		if resolved[name] != value {
			t.Errorf("unexpected resolved value for %s: %q", name, resolved[name])
		}
	}
}

func Test_setupConfigFileArrays(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"hosts": ["alpha", "beta"], "ports": [80, 443], "empty": []}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := &struct {
		Hosts []string `config:"hosts"`
		Ports []uint16 `config:"ports"`
		Empty []int    `config:"empty"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !slices.Equal(config.Hosts, []string{"alpha", "beta"}) {
		t.Errorf("unexpected hosts: %v", config.Hosts)
	} else if !slices.Equal(config.Ports, []uint16{80, 443}) {
		t.Errorf("unexpected ports: %v", config.Ports)
	} else if config.Empty == nil || len(config.Empty) != 0 {
		t.Errorf("expected an empty slice: %v", config.Empty)
	}
}

func Test_setupConfigFileObject(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"server": {"host": "localhost"}}`)

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an object in the configuration file")
	} else if service != nil {
		t.Error("expected no service for an object in the configuration file")
	}
}

func Test_setupNoConfigFile(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "filehost"}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if host := loadHost(t, service); host != "" {
		t.Errorf("expected the configuration file to be ignored: %q", host)
	} else if resolved := service.AllProperties(); len(resolved) != 2 || resolved["root"] != rootDir || resolved["resource"] != testResourceDir {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_setupNoConfigFileUnreadableResource(t *testing.T) {
	// with no configuration file requested the resource directory is never inspected
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, testResourceDir), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func Test_setupMissingConfigFile(t *testing.T) {
	rootDir := t.TempDir()

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, "missing.json", CamelNamingScheme()); err == nil {
		t.Error("expected an error for a missing configuration file")
	} else if !strings.Contains(err.Error(), filepath.Join(rootDir, testResourceDir, "missing.json")) {
		t.Errorf("expected the configuration file name in the error: %v", err)
	} else if service != nil {
		t.Error("expected no service for a missing configuration file")
	}
}

func Test_setupResourceOutsideRoot(t *testing.T) {
	rootDir := t.TempDir()
	for _, resource := range []string{"/etc", rootDir, "..", "../etc", "etc/../..", "./../etc"} {
		if service, err := Setup(testAppName, []string{"root=" + rootDir, "resource=" + resource}, NoConfigFile, CamelNamingScheme()); err == nil {
			t.Errorf("expected an error for resource directory %q", resource)
		} else if service != nil {
			t.Errorf("expected no service for resource directory %q", resource)
		}
	}
}

func Test_setupResourceWithinRoot(t *testing.T) {
	rootDir := t.TempDir()
	cases := map[string]string{
		"":          rootDir,
		".":         rootDir,
		"conf":      filepath.Join(rootDir, "conf"),
		"conf/app":  filepath.Join(rootDir, "conf", "app"),
		"conf/../x": filepath.Join(rootDir, "x"),
		"./conf/":   filepath.Join(rootDir, "conf"),
	}
	for resource, expected := range cases {
		if service, err := Setup(testAppName, []string{"root=" + rootDir, "resource=" + resource}, NoConfigFile, CamelNamingScheme()); err != nil {
			t.Errorf("unexpected error for resource directory %q: %v", resource, err)
		} else if service.ResourceDir() != expected {
			t.Errorf("unexpected resource directory for %q: %s", resource, service.ResourceDir())
		}
	}
}

func Test_setupConfigFileOutsideRoot(t *testing.T) {
	rootDir := t.TempDir()
	outsideFile := filepath.Join(t.TempDir(), testConfigFile)
	if err := os.WriteFile(outsideFile, []byte(`{"host": "outside"}`), 0600); err != nil {
		t.Fatal(err)
	}

	for _, configFile := range []string{outsideFile, "/" + testConfigFile, "../../" + testConfigFile, "../conf/../../" + testConfigFile} {
		if service, err := Setup(testAppName, []string{"root=" + rootDir}, configFile, CamelNamingScheme()); err == nil {
			t.Errorf("expected an error for configuration file %q", configFile)
		} else if !strings.Contains(err.Error(), "within the root directory") {
			t.Errorf("unexpected error for configuration file %q: %v", configFile, err)
		} else if service != nil {
			t.Errorf("expected no service for configuration file %q", configFile)
		}
	}
}

func Test_setupConfigFileOutsideResourceWithinRoot(t *testing.T) {
	// the configuration file is relative to the resource directory, but only needs to be within the root
	rootDir := t.TempDir()
	writeConfigFile(t, rootDir, "shared", testConfigFile, `{"host": "sharedhost"}`)
	writeConfigFile(t, rootDir, filepath.Join(testResourceDir, "sub"), "app.json", `{"host": "subhost"}`)

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, "../shared/"+testConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if host := loadHost(t, service); host != "sharedhost" {
		t.Errorf("expected host from the shared configuration file: %q", host)
	}
	if service, err := Setup(testAppName, []string{"root=" + rootDir}, "sub/app.json", CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if host := loadHost(t, service); host != "subhost" {
		t.Errorf("expected host from the nested configuration file: %q", host)
	}
}

func Test_setupCustomConfigFile(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "defaulthost"}`)
	writeConfigFile(t, rootDir, testResourceDir, "app.json", `{"host": "apphost"}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir}, "app.json", CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if host := loadHost(t, service); host != "apphost" {
		t.Errorf("expected host from the named configuration file: %q", host)
	}
}

func Test_setupResourceFromList(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "etchost"}`)
	writeConfigFile(t, rootDir, "conf", testConfigFile, `{"host": "confhost"}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir, "resource=conf"}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if service.ResourceDir() != filepath.Join(rootDir, "conf") {
		t.Errorf("unexpected resource directory: %s", service.ResourceDir())
	} else if host := loadHost(t, service); host != "confhost" {
		t.Errorf("expected host from the configured resource directory: %q", host)
	} else if resolved := service.AllProperties(); resolved["resource"] != "conf" {
		t.Errorf("unexpected resolved properties: %v", resolved)
	}
}

func Test_setupResourceFromEnvironment(t *testing.T) {
	rootDir := t.TempDir()
	writeConfigFile(t, rootDir, filepath.Join("conf", "app"), testConfigFile, `{"host": "confhost"}`)
	t.Setenv("CONFIGTEST_RESOURCE", filepath.Join("conf", "app"))

	service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if service.ResourceDir() != filepath.Join(rootDir, "conf", "app") {
		t.Errorf("unexpected resource directory: %s", service.ResourceDir())
	} else if host := loadHost(t, service); host != "confhost" {
		t.Errorf("expected host from the configured resource directory: %q", host)
	}
}

func Test_setupConfigFileResource(t *testing.T) {
	// the resource directory is resolved before the configuration file is read, so a
	// resource property in the file must not change the resource directory
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"resource": "elsewhere"}`)

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.ResourceDir() != filepath.Join(rootDir, testResourceDir) {
		t.Errorf("unexpected resource directory: %s", service.ResourceDir())
	}
}

func Test_setupConfigFileRoot(t *testing.T) {
	// the root directory is resolved before the configuration file is read, so a root
	// property in the file must not change the root directory
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"root": "/elsewhere"}`)

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != rootDir {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	}
}

func Test_setupInvalidConfigFile(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"name": `)

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an invalid configuration file")
	} else if service != nil {
		t.Error("expected no service for an invalid configuration file")
	}
}

func Test_setupInvalidConfigFileName(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"not_camel": "value"}`)

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an invalid property name in the configuration file")
	} else if service != nil {
		t.Error("expected no service for an invalid property name in the configuration file")
	}
}

func Test_setupUnreadableConfigFile(t *testing.T) {
	// making the resource directory a regular file means the configuration file
	// can't be checked, which is different from it not existing
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, testResourceDir), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for an unreadable configuration file")
	} else if !strings.Contains(err.Error(), filepath.Join(rootDir, testResourceDir, testConfigFile)) {
		t.Errorf("expected the configuration file name in the error: %v", err)
	} else if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected the underlying cause in the error: %v", err)
	} else if service != nil {
		t.Error("expected no service for an unreadable configuration file")
	}
}

func Test_setupSnakeScheme(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("CONFIGTEST_LISTEN_HOST", "envhost")

	service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, SnakeNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := &struct {
		ListenHost string `config:"listen_host"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.ListenHost != "envhost" {
		t.Errorf("unexpected listen host: %q", config.ListenHost)
	}
}

func Test_setupCamelSchemeEnvironment(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("CONFIGTEST_LISTEN_HOST", "envhost")

	service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := &struct {
		ListenHost string `config:"listenHost"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.ListenHost != "envhost" {
		t.Errorf("unexpected listen host: %q", config.ListenHost)
	}
}

func Test_serviceDirectories(t *testing.T) {
	service := &configService{rootDirectory: "/opt/app", resourceDirectory: "/opt/app/etc"}

	if service.RootDir() != "/opt/app" {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	} else if service.ResourceDir() != "/opt/app/etc" {
		t.Errorf("unexpected resource directory: %s", service.ResourceDir())
	}
}

func Test_serviceLoadConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("localhost", true)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil)
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{"root": "/opt/app"}, hiddenProperties: map[string]string{}}

	config := &innerConfig{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Host != "localhost" {
		t.Errorf("unexpected host: %q", config.Host)
	} else if resolved := service.AllProperties(); len(resolved) != 2 || resolved["root"] != "/opt/app" || resolved["host"] != "localhost" {
		t.Errorf("unexpected resolved properties: %v", resolved)
	} else if visible := service.VisibleProperties(); len(visible) != 2 {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if hidden := service.HiddenProperties(); len(hidden) != 0 {
		t.Errorf("unexpected hidden properties: %v", hidden)
	}
}

func Test_serviceLoadHiddenConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("localhost", true)
	provider.EXPECT().Value("password").Return("secret", true)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil)
	scheme.EXPECT().Validate("password").Return(nil)
	service := &configService{provider: provider, scheme: scheme,
		visibleProperties: map[string]string{"root": "/opt/app"}, hiddenProperties: map[string]string{}}

	config := &struct {
		Host     string `config:"host"`
		Password string `hiddenconfig:"password"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Host != "localhost" || config.Password != "secret" {
		t.Errorf("unexpected populated struct: %+v", *config)
	} else if visible := service.VisibleProperties(); len(visible) != 2 || visible["root"] != "/opt/app" || visible["host"] != "localhost" {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if hidden := service.HiddenProperties(); len(hidden) != 1 || hidden["password"] != "secret" {
		t.Errorf("unexpected hidden properties: %v", hidden)
	} else if all := service.AllProperties(); len(all) != 3 || all["root"] != "/opt/app" || all["host"] != "localhost" || all["password"] != "secret" {
		t.Errorf("unexpected all properties: %v", all)
	}
}

func Test_setupHiddenProperties(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"user": "admin", "token": "filetoken"}`)
	t.Setenv("CONFIGTEST_PASSWORD", "envsecret")

	service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := &struct {
		User     string `config:"user"`
		Password string `hiddenconfig:"password"`
		Token    string `hiddenconfig:"token"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.User != "admin" || config.Password != "envsecret" || config.Token != "filetoken" {
		t.Errorf("unexpected populated struct: %+v", *config)
	} else if visible := service.VisibleProperties(); len(visible) != 3 || visible["root"] != rootDir || visible["resource"] != testResourceDir || visible["user"] != "admin" {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if hidden := service.HiddenProperties(); len(hidden) != 2 || hidden["password"] != "envsecret" || hidden["token"] != "filetoken" {
		t.Errorf("unexpected hidden properties: %v", hidden)
	} else if all := service.AllProperties(); len(all) != 5 {
		t.Errorf("unexpected all properties: %v", all)
	}
}

func Test_serviceLoadConfigError(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{"root": "/opt/app"}, hiddenProperties: map[string]string{}}

	if err := service.LoadConfig(innerConfig{}); err == nil {
		t.Error("expected an error for a non-pointer config")
	} else if resolved := service.AllProperties(); len(resolved) != 1 {
		t.Errorf("resolved properties changed after error: %v", resolved)
	} else if hidden := service.HiddenProperties(); len(hidden) != 0 {
		t.Errorf("hidden properties changed after error: %v", hidden)
	}
}

func Test_servicePropertiesIsolated(t *testing.T) {
	service := &configService{visibleProperties: map[string]string{"root": "/opt/app"}, hiddenProperties: map[string]string{"secret": "value"}}

	for _, properties := range []map[string]string{service.AllProperties(), service.VisibleProperties(), service.HiddenProperties()} {
		properties["root"] = "/changed"
		properties["secret"] = "changed"
		properties["added"] = "value"
	}

	if visible := service.VisibleProperties(); len(visible) != 1 || visible["root"] != "/opt/app" {
		t.Errorf("visible properties were affected by caller: %v", visible)
	} else if hidden := service.HiddenProperties(); len(hidden) != 1 || hidden["secret"] != "value" {
		t.Errorf("hidden properties were affected by caller: %v", hidden)
	} else if all := service.AllProperties(); len(all) != 2 || all["root"] != "/opt/app" || all["secret"] != "value" {
		t.Errorf("all properties were affected by caller: %v", all)
	}
}

func Test_serviceConcurrentLoadConfig(t *testing.T) {
	tagName := func(i int) string {
		if i%2 == 0 {
			return PublicTagName
		}
		return PrivateTagName
	}

	const loaders = 50
	properties := map[string]string{}
	for i := range loaders {
		properties["property"+strconv.Itoa(i)] = strconv.Itoa(i)
	}
	service := &configService{provider: MapProvider(properties), scheme: CamelNamingScheme(), visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	// each loader populates its own struct type, tagged with a distinct property that
	// alternates between visible and hidden, while other loaders are updating and
	// reading the shared resolved properties
	var wg sync.WaitGroup
	for i := range loaders {
		wg.Go(func() {
			configType := reflect.StructOf([]reflect.StructField{{
				Name: "Value",
				Type: reflect.TypeFor[int](),
				Tag:  reflect.StructTag(tagName(i) + `:"property` + strconv.Itoa(i) + `"`),
			}})
			config := reflect.New(configType)
			if err := service.LoadConfig(config.Interface()); err != nil {
				t.Errorf("unexpected error: %v", err)
			} else if value := config.Elem().Field(0).Int(); value != int64(i) {
				t.Errorf("unexpected value for loader %d: %d", i, value)
			}
			_ = service.AllProperties()
			_ = service.VisibleProperties()
			_ = service.HiddenProperties()
		})
	}
	wg.Wait()

	if visible := service.VisibleProperties(); len(visible) != loaders/2 {
		t.Errorf("unexpected number of visible properties: %d", len(visible))
	} else if hidden := service.HiddenProperties(); len(hidden) != loaders/2 {
		t.Errorf("unexpected number of hidden properties: %d", len(hidden))
	}
	if resolved := service.AllProperties(); len(resolved) != loaders {
		t.Errorf("unexpected number of resolved properties: %d", len(resolved))
	} else {
		for name, value := range properties {
			if resolved[name] != value {
				t.Errorf("unexpected resolved value for %s: %q", name, resolved[name])
			}
		}
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func Test_setupConfigFileLinkOutsideRoot(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	writeConfigFile(t, outsideDir, "", testConfigFile, `{"host": "outside"}`)
	symlink(t, filepath.Join(outsideDir, testConfigFile), filepath.Join(rootDir, testResourceDir, testConfigFile))

	if service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a configuration file linked outside the root")
	} else if !strings.Contains(err.Error(), "outside") {
		t.Errorf("unexpected error: %v", err)
	} else if service != nil {
		t.Error("expected no service for a configuration file linked outside the root")
	}
}

func Test_setupResourceLinkOutsideRoot(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	writeConfigFile(t, outsideDir, "", testConfigFile, `{"host": "outside"}`)
	symlink(t, outsideDir, filepath.Join(rootDir, testResourceDir))

	// the resource directory is rejected whether or not a configuration file is requested
	for _, configFile := range []string{testConfigFile, NoConfigFile} {
		if service, err := Setup(testAppName, []string{"root=" + rootDir}, configFile, CamelNamingScheme()); err == nil {
			t.Errorf("expected an error for a resource directory linked outside the root (config file %q)", configFile)
		} else if service != nil {
			t.Errorf("expected no service for a resource directory linked outside the root (config file %q)", configFile)
		}
	}
}

func Test_setupIntermediateLinkOutsideRoot(t *testing.T) {
	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	writeConfigFile(t, outsideDir, "app", testConfigFile, `{"host": "outside"}`)
	symlink(t, outsideDir, filepath.Join(rootDir, "conf"))

	if _, err := Setup(testAppName, []string{"root=" + rootDir, "resource=conf/app"}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a resource directory reached through a link outside the root")
	}
	if _, err := Setup(testAppName, []string{"root=" + rootDir}, "../conf/app/"+testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a configuration file reached through a link outside the root")
	}
}

func Test_setupLinksWithinRoot(t *testing.T) {
	rootDir := t.TempDir()
	writeConfigFile(t, rootDir, "shared", testConfigFile, `{"host": "sharedhost"}`)
	symlink(t, filepath.Join(rootDir, "shared"), filepath.Join(rootDir, testResourceDir))
	symlink(t, filepath.Join("..", "shared", testConfigFile), filepath.Join(rootDir, "shared", "linked.json"))

	for _, configFile := range []string{testConfigFile, "linked.json"} {
		if service, err := Setup(testAppName, []string{"root=" + rootDir}, configFile, CamelNamingScheme()); err != nil {
			t.Errorf("unexpected error for %q: %v", configFile, err)
		} else if host := loadHost(t, service); host != "sharedhost" {
			t.Errorf("unexpected host for %q: %q", configFile, host)
		}
	}
}

func Test_setupRootThroughLink(t *testing.T) {
	realRoot := t.TempDir()
	writeResourceConfig(t, realRoot, `{"host": "roothost"}`)
	linkedRoot := filepath.Join(t.TempDir(), "root")
	symlink(t, realRoot, linkedRoot)

	if service, err := Setup(testAppName, []string{"root=" + linkedRoot}, testConfigFile, CamelNamingScheme()); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if service.RootDir() != linkedRoot {
		t.Errorf("unexpected root directory: %s", service.RootDir())
	} else if host := loadHost(t, service); host != "roothost" {
		t.Errorf("unexpected host: %q", host)
	}
}

func Test_setupDanglingConfigFileLink(t *testing.T) {
	rootDir := t.TempDir()
	symlink(t, filepath.Join(rootDir, "nowhere.json"), filepath.Join(rootDir, testResourceDir, testConfigFile))

	if _, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme()); err == nil {
		t.Error("expected an error for a dangling configuration file link")
	} else if !strings.Contains(err.Error(), "missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func Test_serviceKnownProperties(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().KnownProperties().Return(map[string]string{"root": "/opt/app", "host": "localhost"})
	scheme := mocks.NewMockNamingScheme(ctrl)
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	if known := service.KnownProperties(); !maps.Equal(known, map[string]string{"root": "/opt/app", "host": "localhost"}) {
		t.Errorf("unexpected known properties: %v", known)
	}
}

func Test_setupKnownProperties(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "filehost", "port": 1000, "unused": "value"}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir, "host=listhost"}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// known properties include everything configured, whether or not it has been loaded, with
	// the same priority as values that are loaded
	expected := map[string]string{"root": rootDir, "host": "listhost", "port": "1000", "unused": "value"}
	if known := service.KnownProperties(); !maps.Equal(known, expected) {
		t.Errorf("unexpected known properties: %v", known)
	} else if host := loadHost(t, service); host != known["host"] {
		t.Errorf("known properties disagree with loaded host: %q", host)
	}
}

func Test_setupKnownPropertiesNoConfigFile(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "filehost"}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if known := service.KnownProperties(); !maps.Equal(known, map[string]string{"root": rootDir}) {
		t.Errorf("unexpected known properties: %v", known)
	}
}

func Test_setupKnownPropertiesIsolated(t *testing.T) {
	rootDir := t.TempDir()
	service, err := Setup(testAppName, []string{"root=" + rootDir, "host=listhost"}, NoConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	known := service.KnownProperties()
	known["host"] = "changed"
	known["added"] = "value"

	if again := service.KnownProperties(); len(again) != 2 || again["host"] != "listhost" {
		t.Errorf("known properties were affected by caller: %v", again)
	} else if host := loadHost(t, service); host != "listhost" {
		t.Errorf("loaded host was affected by caller: %q", host)
	}
}

func Test_serviceLoadProperty(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("localhost", true)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil)
	service := &configService{provider: provider, scheme: scheme,
		visibleProperties: map[string]string{"root": "/opt/app"}, hiddenProperties: map[string]string{}}

	if value, present, err := service.LoadProperty("host"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !present || value != "localhost" {
		t.Errorf("unexpected value: %q (%t)", value, present)
	} else if visible := service.VisibleProperties(); len(visible) != 2 || visible["root"] != "/opt/app" || visible["host"] != "localhost" {
		t.Errorf("unexpected visible properties: %v", visible)
	} else if hidden := service.HiddenProperties(); len(hidden) != 0 {
		t.Errorf("unexpected hidden properties: %v", hidden)
	} else if all := service.AllProperties(); len(all) != 2 || all["host"] != "localhost" {
		t.Errorf("unexpected all properties: %v", all)
	}
}

func Test_serviceLoadPropertyEmptyValue(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("", true)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil)
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	if value, present, err := service.LoadProperty("host"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !present || value != "" {
		t.Errorf("unexpected value: %q (%t)", value, present)
	} else if visible := service.VisibleProperties(); len(visible) != 1 {
		t.Errorf("expected an empty value to be recorded: %v", visible)
	} else if value, recorded := visible["host"]; !recorded || value != "" {
		t.Errorf("unexpected recorded value: %q (%t)", value, recorded)
	}
}

func Test_serviceLoadPropertyMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("", false)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil)
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	if value, present, err := service.LoadProperty("host"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if present || value != "" {
		t.Errorf("unexpected value: %q (%t)", value, present)
	} else if all := service.AllProperties(); len(all) != 0 {
		t.Errorf("expected nothing to be recorded for a missing property: %v", all)
	}
}

func Test_serviceLoadPropertyRepeated(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := mocks.NewMockProvider(ctrl)
	provider.EXPECT().Value("host").Return("localhost", true).Times(2)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("host").Return(nil).Times(2)
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	for range 2 {
		if value, present, err := service.LoadProperty("host"); err != nil {
			t.Errorf("unexpected error: %v", err)
		} else if !present || value != "localhost" {
			t.Errorf("unexpected value: %q (%t)", value, present)
		}
	}
	if visible := service.VisibleProperties(); len(visible) != 1 || visible["host"] != "localhost" {
		t.Errorf("unexpected visible properties: %v", visible)
	}
}

func Test_setupLoadProperty(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "filehost", "port": 1000, "names": ["a", "b"], "name": "file"}`)
	t.Setenv("CONFIGTEST_PORT", "2000")
	t.Setenv("CONFIGTEST_NAME", "env")

	service, err := Setup(testAppName, []string{"root=" + rootDir, "host=listhost"}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// values follow the same order of preference as LoadConfig, and are the raw string encoding
	expected := map[string]string{"host": "listhost", "port": "2000", "name": "env", "names": strings.Join([]string{"a", "b"}, SliceValueSeparator)}
	for name, expectedValue := range expected {
		if value, present, err := service.LoadProperty(name); err != nil {
			t.Errorf("unexpected error: %v", err)
		} else if !present || value != expectedValue {
			t.Errorf("unexpected value for %s: %q (%t)", name, value, present)
		}
	}
	if _, present, err := service.LoadProperty("missing"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if present {
		t.Error("expected no value for a missing property")
	}

	visible := service.VisibleProperties()
	for name, expectedValue := range expected {
		if visible[name] != expectedValue {
			t.Errorf("unexpected recorded value for %s: %q", name, visible[name])
		}
	}
	if _, recorded := visible["missing"]; recorded {
		t.Error("missing property was recorded")
	}
}

func Test_setupLoadPropertyMatchesLoadConfig(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"host": "filehost"}`)

	service, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if value, present, err := service.LoadProperty("host"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !present || value != loadHost(t, service) {
		t.Errorf("LoadProperty disagrees with LoadConfig: %q (%t)", value, present)
	}
}

func Test_serviceConcurrentLoadProperty(t *testing.T) {
	const loaders = 50
	properties := map[string]string{}
	for i := range loaders {
		properties["property"+strconv.Itoa(i)] = strconv.Itoa(i)
	}
	service := &configService{provider: MapProvider(properties), scheme: CamelNamingScheme(), visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	var wg sync.WaitGroup
	for i := range loaders {
		wg.Go(func() {
			name := "property" + strconv.Itoa(i)
			if value, present, err := service.LoadProperty(name); err != nil {
				t.Errorf("unexpected error: %v", err)
			} else if !present || value != strconv.Itoa(i) {
				t.Errorf("unexpected value for %s: %q (%t)", name, value, present)
			}
			if err := service.LoadConfig(&innerConfig{}); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			_ = service.VisibleProperties()
			_ = service.AllProperties()
		})
	}
	wg.Wait()

	if visible := service.VisibleProperties(); !maps.Equal(visible, properties) {
		t.Errorf("unexpected visible properties: %v", visible)
	}
}

func Test_serviceLoadPropertyInvalidName(t *testing.T) {
	ctrl := gomock.NewController(t)
	// the provider has no expectations, so the test fails if it's asked for an invalid name
	provider := mocks.NewMockProvider(ctrl)
	scheme := mocks.NewMockNamingScheme(ctrl)
	scheme.EXPECT().Validate("Host").Return(errors.New("invalid"))
	service := &configService{provider: provider, scheme: scheme, visibleProperties: map[string]string{}, hiddenProperties: map[string]string{}}

	if value, present, err := service.LoadProperty("Host"); err == nil {
		t.Error("expected an error for an invalid property name")
	} else if present || value != "" {
		t.Errorf("unexpected value for an invalid property name: %q (%t)", value, present)
	} else if all := service.AllProperties(); len(all) != 0 {
		t.Errorf("expected nothing to be recorded for an invalid property name: %v", all)
	}
}

func Test_setupLoadPropertyInvalidName(t *testing.T) {
	rootDir := t.TempDir()
	// each of these would be found in the environment if the name weren't checked first
	t.Setenv("CONFIGTEST_ROOT_DIR", "/snake")
	t.Setenv("CONFIGTEST_ROOT-DIR", "/kebab")
	t.Setenv("CONFIGTEST_HOST", "envhost")

	service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, CamelNamingScheme())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"root_dir", "root-dir", "Host", "", "2host", "host name"} {
		if value, present, err := service.LoadProperty(name); err == nil {
			t.Errorf("expected an error for invalid property name %q", name)
		} else if present || value != "" {
			t.Errorf("unexpected value for invalid property name %q: %q (%t)", name, value, present)
		}
	}
	if visible := service.VisibleProperties(); len(visible) != 2 || visible["root"] != rootDir || visible["resource"] != testResourceDir {
		t.Errorf("invalid property names were recorded: %v", visible)
	}

	// a valid name is still resolved after the failures
	if value, present, err := service.LoadProperty("host"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !present || value != "envhost" {
		t.Errorf("unexpected value: %q (%t)", value, present)
	}
}

func Test_setupLoadPropertyFollowsScheme(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("CONFIGTEST_LISTEN_HOST", "envhost")

	cases := map[string]struct {
		scheme         NamingScheme
		valid, invalid string
	}{
		"camel": {scheme: CamelNamingScheme(), valid: "listenHost", invalid: "listen_host"},
		"snake": {scheme: SnakeNamingScheme(), valid: "listen_host", invalid: "listenHost"},
		"kebab": {scheme: KebabNamingScheme(), valid: "listen-host", invalid: "listen_host"},
	}
	for schemeName, test := range cases {
		service, err := Setup(testAppName, []string{"root=" + rootDir}, NoConfigFile, test.scheme)
		if err != nil {
			t.Fatalf("unexpected error for scheme %s: %v", schemeName, err)
		}

		if value, present, err := service.LoadProperty(test.valid); err != nil {
			t.Errorf("unexpected error for %q with scheme %s: %v", test.valid, schemeName, err)
		} else if !present || value != "envhost" {
			t.Errorf("unexpected value for %q with scheme %s: %q (%t)", test.valid, schemeName, value, present)
		}
		if _, _, err := service.LoadProperty(test.invalid); err == nil {
			t.Errorf("expected an error for %q with scheme %s", test.invalid, schemeName)
		}
	}
}

// dottedScheme is a custom NamingScheme, of the kind a developer might write, for names
// like "http.port"
type dottedScheme struct{}

func (dottedScheme) Components(propertyName string) []string {
	return strings.Split(propertyName, ".")
}

func (dottedScheme) Validate(propertyName string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`).MatchString(propertyName) {
		return fmt.Errorf("property name is invalid: %s", propertyName)
	}
	return nil
}

func Test_setupCustomScheme(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"http.host": "filehost", "http.port": 1000}`)
	t.Setenv("CONFIGTEST_HTTP_PORT", "2000")

	service, err := Setup(testAppName, []string{"root=" + rootDir, "log.level=debug"}, testConfigFile, dottedScheme{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// the custom scheme is used for struct tags, the environment, the list and the file
	config := &struct {
		Host  string `config:"http.host"`
		Port  int    `config:"http.port"`
		Level string `config:"log.level"`
	}{}
	if err := service.LoadConfig(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if config.Host != "filehost" || config.Port != 2000 || config.Level != "debug" {
		t.Errorf("unexpected populated struct: %+v", *config)
	}

	if value, present, err := service.LoadProperty("http.port"); err != nil {
		t.Errorf("unexpected error: %v", err)
	} else if !present || value != "2000" {
		t.Errorf("unexpected value: %q (%t)", value, present)
	}
	if _, _, err := service.LoadProperty("httpPort"); err == nil {
		t.Error("expected an error for a name that isn't valid for the custom scheme")
	}
}

func Test_setupCustomSchemeRejectsInvalidNames(t *testing.T) {
	rootDir := t.TempDir()
	writeResourceConfig(t, rootDir, `{"httpPort": 1000}`)

	if _, err := Setup(testAppName, []string{"root=" + rootDir, "logLevel=debug"}, NoConfigFile, dottedScheme{}); err == nil {
		t.Error("expected an error for a list property that isn't valid for the custom scheme")
	}
	if _, err := Setup(testAppName, []string{"root=" + rootDir}, testConfigFile, dottedScheme{}); err == nil {
		t.Error("expected an error for a file property that isn't valid for the custom scheme")
	}
}
