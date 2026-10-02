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
	"sync"
)

// NoConfigFile is used to tell the service there is no config file.
const NoConfigFile = ""

// BootstrapConfig contains the configuration that the service will load through
// the library itself to bootstrap initialization.
type BootstrapConfig struct {
	RootDirectory        string `config:"root"`
	ResourceSubdirectory string `config:"resource,etc"`
}

// configService is an implementation of Service that can be used for production.
type configService struct {
	rootDirectory     string
	resourceDirectory string
	provider          Provider
	scheme            NamingScheme
	visibleProperties map[string]string
	hiddenProperties  map[string]string
	resolvedLock      sync.RWMutex
}

// Setup initializes a Service that has all the expected behaviors of a production
// configuration system. It provides configuration from (in-preference) the given kvElements,
// the environment, and a backing file. The latter should be under the ResourceSubdirectory so
// the value of configFile is a name relative to that directory. Use the value NoConfigFile to
// indicate that that there is no file to load. Note that only a flat JSON file is currently
// supported.
//
// The resource directory must be under the directory named by RootDirectory, and should be
// provided as a relative path to the root directory. If no value for the property root is
// provided, then the parent of the directory containing the running binary is used. For
// instance, if this library is used in a program in the directory "/app/bin/"", and no root
// property is provided, then the value of RootDirectory will be "/app".
//
// The values in kvElements are name-value pairs of the form "NAME=VALUE". These are typically
// derived from a process' argument list, or are a known set of properties that should override
// any other values.
//
// The value of name affects how environment variables are resolved. See EnvironmentProvider()
// for details about how this works. Note that all property names accepted and reported by this
// service will maintain the form valid against the scheme, regardless of whether the
// actual value resolution used a different form of the name.
//
// An error is returned if the root or resource directories can't be resolved, if the
// config file is not accessible within the resource directory or is not a valid flat JSON
// file, if any property name in kvElements or the config file does not conform with the
// scheme, if scheme is nil, or if any other aspect of setup fails. Once a Service has
// been set up it cannot be modified, and is safe to use concurrently.
func Setup(name string, kvElements []string, configFile string, scheme NamingScheme) (Service, error) {
	// make sure that we have a valid scheme
	if scheme == nil {
		return nil, fmt.Errorf("cannot setup a service with a nil NamingScheme")
	}

	// bootstrap configuration from the command-line and environment
	commandLine, err := ListProvider(kvElements, scheme)
	if err != nil {
		return nil, err
	}
	environment, err := EnvironmentProvider(name, scheme)
	if err != nil {
		return nil, err
	}
	chain := ChainedProvider(commandLine, environment)

	// use what we have at this point to load the bootstrap configuration
	service := &configService{
		provider:          chain,
		scheme:            scheme,
		visibleProperties: map[string]string{},
		hiddenProperties:  map[string]string{},
	}
	bootstrapConfig := &BootstrapConfig{}
	if err := service.LoadConfig(bootstrapConfig); err != nil {
		return nil, err
	}

	// resolve the root directory based on configuration or from our binary location
	if bootstrapConfig.RootDirectory == "" {
		if absBinaryPath, err := filepath.Abs(os.Args[0]); err != nil {
			return nil, fmt.Errorf("failed to resolve root directory: %s", err.Error())
		} else {
			binDir, _ := filepath.Split(absBinaryPath)
			bootstrapConfig.RootDirectory, _ = filepath.Split(filepath.Clean(binDir))
		}
	}
	service.rootDirectory = filepath.Clean(bootstrapConfig.RootDirectory)

	// the root directory must already exist, which catches a mistyped root even when no
	// configuration file is requested
	if info, err := os.Stat(service.rootDirectory); err != nil {
		return nil, fmt.Errorf("root directory %s is not accessible: %s", service.rootDirectory, err.Error())
	} else if !info.IsDir() {
		return nil, fmt.Errorf("root directory %s is not a directory", service.rootDirectory)
	}

	// the resource directory must be within the root directory, where an empty value is the
	// root directory itself
	if !isWithinRoot(bootstrapConfig.ResourceSubdirectory) {
		return nil, fmt.Errorf("resource directory must be a relative path within the root directory: %s", bootstrapConfig.ResourceSubdirectory)
	}
	service.resourceDirectory = filepath.Join(service.rootDirectory, bootstrapConfig.ResourceSubdirectory)

	// the resource directory doesn't need to exist, but if it does then it can't be a link
	// that resolves to somewhere outside of the root directory
	if _, err := os.Lstat(service.resourceDirectory); err == nil {
		if err := checkResolvesWithinRoot(service.rootDirectory, service.resourceDirectory); err != nil {
			return nil, fmt.Errorf("resource directory must be within the root directory: %s", err.Error())
		}
	}

	// see if the config file is available, and if it is attempt to process
	// it & chain it off the end of the current providers
	if configFile != NoConfigFile {
		// the config file is relative to the resource directory, but may be anywhere within the root directory
		if filepath.IsAbs(configFile) || !isWithinRoot(filepath.Join(bootstrapConfig.ResourceSubdirectory, configFile)) {
			return nil, fmt.Errorf("config file must be a relative path within the root directory: %s", configFile)
		}
		configFilename := filepath.Join(service.resourceDirectory, configFile)
		if _, err := os.Stat(configFilename); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				// this means it exists but can't be read so this should be raised as an issue
				return nil, fmt.Errorf("could not read existing configuration file %s: %s", configFilename, err.Error())
			} else {
				return nil, fmt.Errorf("requested config file %s is missing", configFilename)
			}
		} else if err := checkResolvesWithinRoot(service.rootDirectory, configFilename); err != nil {
			return nil, fmt.Errorf("config file must be within the root directory: %s", err.Error())
		} else if provider, err := FlatJSONProvider(configFilename, scheme); err != nil {
			return nil, err
		} else {
			service.provider = ChainedProvider(service.provider, provider)
		}
	}

	return service, nil
}

func isWithinRoot(relativePath string) bool {
	// IsLocal rejects absolute paths and any path that, once cleaned, reaches above its
	// starting point, but also rejects the empty path which here refers to the root itself
	return relativePath == "" || filepath.IsLocal(relativePath)
}

func checkResolvesWithinRoot(rootDirectory, path string) error {
	// the textual checks can't see where symbolic links lead, so resolve both the root (which
	// may itself be reached through a link) and the path, and compare where they really are
	realRoot, err := filepath.EvalSymlinks(rootDirectory)
	if err != nil {
		return fmt.Errorf("could not resolve root directory %s: %s", rootDirectory, err.Error())
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("could not resolve %s: %s", path, err.Error())
	}
	if relativePath, err := filepath.Rel(realRoot, realPath); err != nil || !isWithinRoot(relativePath) {
		return fmt.Errorf("%s resolves to %s which is outside of %s", path, realPath, realRoot)
	}
	return nil
}

/* Implement Service */

func (cs *configService) RootDir() string {
	return cs.rootDirectory
}

func (cs *configService) ResourceDir() string {
	return cs.resourceDirectory
}

func (cs *configService) LoadConfig(configStruct any) error {
	// the providers are read-only once setup is complete, so only the update to the
	// shared resolved properties needs to be protected
	if visible, hidden, err := PopulateStruct(configStruct, cs.provider, cs.scheme); err != nil {
		return err
	} else {
		cs.resolvedLock.Lock()
		defer cs.resolvedLock.Unlock()

		maps.Copy(cs.visibleProperties, visible)
		maps.Copy(cs.hiddenProperties, hidden)
		return nil
	}
}

func (cs *configService) LoadProperty(propertyName string) (string, bool, error) {
	if err := cs.scheme.Validate(propertyName); err != nil {
		return "", false, err
	}

	propertyValue, present := cs.provider.Value(propertyName)
	if !present {
		return propertyValue, present, nil
	}

	cs.resolvedLock.Lock()
	defer cs.resolvedLock.Unlock()

	cs.visibleProperties[propertyName] = propertyValue

	return propertyValue, present, nil
}

func (cs *configService) AllProperties() map[string]string {
	cs.resolvedLock.RLock()
	defer cs.resolvedLock.RUnlock()

	allProperties := maps.Clone(cs.visibleProperties)
	maps.Copy(allProperties, cs.hiddenProperties)
	return allProperties
}

func (cs *configService) HiddenProperties() map[string]string {
	cs.resolvedLock.RLock()
	defer cs.resolvedLock.RUnlock()

	return maps.Clone(cs.hiddenProperties)
}

func (cs *configService) VisibleProperties() map[string]string {
	cs.resolvedLock.RLock()
	defer cs.resolvedLock.RUnlock()

	return maps.Clone(cs.visibleProperties)
}

func (cs *configService) KnownProperties() map[string]string {
	return cs.provider.KnownProperties()
}
