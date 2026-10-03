# Tranquil Data's Configuration Library #

This library provides common capabilities to define, validate, resolve, and report application configuration. It is used today by the Tranquil Data products, which accept a large range of configuration across distributed deployments. It's offered to the community to help fill a gap we've seen for enterprise-style configuration management.

## What Is It? ##

This is a self-contained (no external runtime dependencies) library, written in Golang (1.25+), that provides a single point for configuration management. At a high level, it lets you:

* Define and enforce a consistent naming scheme for property keys based on common patterns or your own requirements
* Validate type-correctness for property values
* Resolve property values from a hierarchy of common places like the environment or a config file, or build support for your own sources
* Define each property's name, type, and default value in a single place, in your code
* Report the full set of configuration for a running system
* Separate user-visible and developer-internal configuration
* Compose this project's pieces to suit your requirements, or use our packaged design to get started quickly

## Getting Started ##

Import this project and create a `config.Service` that is used for all configuration loading and reporting:

```go
import "github.com/tranquildata/config"

func CreateService() (config.Service, error) {
    properties := []string{"httpPort=8080"}
    return config.Setup("myapp", properties, config.NoConfigFile, config.CamelNamingScheme())
}
```

This `Service` has static `properties` (often from the command-line or from within an application), does not use a config file, and enforces Camel Case for property names. It falls-back on the environment if asked for a property not in the `properties` slice. For instance, when asked for `requireClientAuth` it will try to resolve the environment variable named `MYAPP_REQUIRE_CLIENT_AUTH`.

Define structs that the `Service` can automatically populate:

```go
import (
    "crypto/tls"
    "net/url"
    "github.com/tranquildata/config"
)

type PortType uint16

type WebConfig struct {
    Port            PortType       `config:"httpPort"`
    Endpoint        url.URL        `config:"httpEndpoint,https://localhost/webapp"`
    UseNewTransport bool           `hiddenconfig:"useNewTransport,false"`
    Security        SecurityConfig `config:""`
    TLS             *tls.Config
}

type SecurityConfig struct {
    ClientAuth     bool     `config:"requireClientAuth,true"`
    Signatures     []string `config:"allowedSignatureAlgs,sha256WithRSA;sha256WithECDSA;sha3_256WithRSA"`
    referenceCount uint64
}

func getWebConfig(service config.Service) (*WebConfig, error) {
    var webConfig WebConfig
    err := service.LoadConfig(&webConfig)
    return &webConfig, err
}
```

The `Service` resolves each field value using the available property sources as described above. It validates the names and the expected types, fills-in the optional default values as-needed, and tracks which properties are visible or hidden. When it finds a struct tagged with `config:""` it recurses to fill-in that structure too, and it leaves untagged structs like `tls.Config` alone.

If `LoadConfig()` doesn't return an error then the config structure has been filled-in with the correct, validated values. As you load structs, you now have a place to retrieve the full, running configuration state:

```go
func getProperties(service config.Service, includeHidden bool) map[string]string {
    if includeHidden {
        return service.AllProperties()
    }
    return service.VisibleProperties()
}
```

## Supported Structure ##

Base types are `string`, `bool`, all int types, all unsigned int types, all float types, `time.Time`, `time.Duration`, and `url.URL`. Any field that is one of these types, a custom type that is defined as one of these types, or a slice of one of these types is supported. Tagged fields may not use pointers to base types, like `*string`.

Slices are separated using semicolons. A slice may have zero or more entries.

A struct field can be another struct, or a pointer to another struct. If the field is exported and tagged with an empty `config:""` tag, it will be recursively loaded. This makes it easy to share common configuration elements across component-specific definitions. An untagged struct field is ignored, so config structs can hold third-party types like `tls.Config` without them being touched. If an untagged struct field's type has its own tagged fields, though, it was most likely meant to be loaded, so an error is raised. Tag the field `config:"-"` to skip it on purpose. Struct fields (other than the base types like `time.Time`) may not be tagged with a property name, or with `hiddenconfig`.

The `config` tag identifies a field that should be loaded. The `hiddenconfig` tag does exactly the same thing, only marking the field as hidden. Any field without a tag is ignored. An error is raised if an un-exported field is tagged.

Naming schemes serve two purposes: they ensure consistency across all property names, and they split property names to generate other forms at runtime (like creating the environment variable format of a configuration property name). The library currently includes support for `Camel`, `Snake`, and `Kebab` schemes, and makes it simple to integrate additional naming schemes as-needed.

## Providers ##

This library defines a generic `Provider` interface to support retrieving property values based on name, and several common implementations:

* `ListProvider` is initialized with a list of `key=value` formatted strings. This is often used to support `-p` command-line flags, or similar, where a static set of properties should be available to the full system.
* `MapProvider` is a map from property name to (string) value. This is often used when an application has a static set of properties that it wants to make available on startup.
* `EnvironmentProvider` is able to convert names based on the naming scheme and resolve property values at runtime from the environment. This is often a fall-back after some initial sources don't provide values.
* `ChainedProvider` attempts to use a `Provider` to resolve a value, and falls-back on a second `Provider` if the first one doesn't return a value. This lets a `Service` configure any number of `Provider`s with clear priority-ordering.
* `FlatJSONProvider` uses a local JSON file as a source of properties, where each key is the name of a property and each value is a base type or array of base type for the property's value. This is typically used as a backing, stable configuration when neither the command-line nor the environment defines a property's value.

The `Service` implementation provided with this library does all the setup and validation to define a root directory and chain (in priority ordering) a list of static properties, properties from the environment, and finally properties from a JSON file. In practice, we've found this fits most common, enterprise configurations. This said, you are free to build your own combination of providers, or define new sources of configuration properties.

## Future Work ##

This library was developed from the internal functionality we built over several years, so it's biased toward what we need to solve. Our products  use this library, so we will be contributing directly as we need new capabilities. If you are interested in contributing, please let us know!

To get you started, here are a few areas that we would like to build (or see contributed):

* NamingScheme implementations for patterns like `dotted.names` or `urn:urns`.
* Provider implementations for file formats that support nested structure to generate hierarchical property names
* Provider implementations for common online sources of configuration
* Support for validating Enums (Golang doesn't have a native Enum type, so we haven't tried to generalize this one yet)
* Support for additional base types that are useful in typical configurations like `LDAP` or `DNS` Names 