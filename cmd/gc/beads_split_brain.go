package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

func validateNoInheritedRigSplitBrain(cityPath string) error {
	cityPath = normalizePathForCompare(cityPath)
	if strings.TrimSpace(cityPath) == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(cityPath, "city.toml")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("split-brain guard: stat city config: %w", err)
	}
	cfg, err := loadCityConfig(cityPath, io.Discard)
	if err != nil {
		return fmt.Errorf("split-brain guard: load city config: %w", err)
	}
	resolveRigPaths(cityPath, cfg.Rigs)
	cityPort := canonicalCityDoltPortHint(cityPath)
	for _, rig := range cfg.Rigs {
		rigPath := strings.TrimSpace(rig.Path)
		if rigPath == "" {
			continue
		}
		rigPath = resolveStoreScopeRoot(cityPath, rigPath)
		inherited, err := rigInheritsCityEndpoint(cityPath, rig, rigPath)
		if err != nil {
			return err
		}
		if !inherited {
			continue
		}
		if err := validateInheritedRigHasNoLocalDoltAuthority(cityPath, rig, rigPath, cityPort); err != nil {
			return err
		}
	}
	return nil
}

func rigInheritsCityEndpoint(cityPath string, rig config.Rig, rigPath string) (bool, error) {
	state, ok, err := contract.ReadConfigState(fsys.OSFS{}, filepath.Join(rigPath, ".beads", "config.yaml"))
	if err != nil {
		return false, fmt.Errorf("split-brain guard: read rig %q beads config: %w", rig.Name, err)
	}
	if ok {
		switch state.EndpointOrigin {
		case contract.EndpointOriginInheritedCity:
			return true, nil
		case contract.EndpointOriginExplicit:
			return false, nil
		}
	}
	if strings.TrimSpace(rig.DoltHost) != "" || strings.TrimSpace(rig.DoltPort) != "" {
		return false, nil
	}
	return scopeUsesManagedBdStoreContract(cityPath, rigPath), nil
}

func validateInheritedRigHasNoLocalDoltAuthority(cityPath string, rig config.Rig, rigPath, cityPort string) error {
	beadsDir := filepath.Join(rigPath, ".beads")
	doltDir := filepath.Join(beadsDir, "dolt")
	rigName := strings.TrimSpace(rig.Name)
	if rigName == "" {
		rigName = filepath.Base(rigPath)
	}
	if cityPort != "" {
		if rigPort, ok, err := readTrimmedFile(filepath.Join(beadsDir, "dolt-server.port")); err != nil {
			return fmt.Errorf("split-brain guard: read rig %q dolt port file: %w", rigName, err)
		} else if ok && rigPort != "" && rigPort != cityPort {
			return fmt.Errorf("split-brain guard: inherited rig %q points at local Dolt port %s, but city canonical port is %s", rigName, rigPort, cityPort)
		}
	}
	if db := canonicalScopeDoltDatabase(cityPath, rigPath, rig.EffectivePrefix()); db != "" {
		if isDir(filepath.Join(doltDir, db, ".dolt")) {
			return fmt.Errorf("split-brain guard: inherited rig %q has a rig-local Dolt database at %s; inherited rigs must use the city Dolt store", rigName, filepath.Join(doltDir, db))
		}
	}
	if dbPath, ok, err := firstLocalDoltDatabaseDir(doltDir); err != nil {
		return fmt.Errorf("split-brain guard: inspect rig %q Dolt directory: %w", rigName, err)
	} else if ok {
		return fmt.Errorf("split-brain guard: inherited rig %q has a rig-local Dolt database at %s; inherited rigs must use the city Dolt store", rigName, dbPath)
	}
	if pid, err := doltSQLServerPIDForDataDir(doltDir); err != nil {
		return fmt.Errorf("split-brain guard: inspect local Dolt processes for rig %q: %w", rigName, err)
	} else if pid > 0 {
		return fmt.Errorf("split-brain guard: inherited rig %q has a live rig-local Dolt sql-server process pid %d using %s", rigName, pid, doltDir)
	}
	return nil
}

func canonicalCityDoltPortHint(cityPath string) string {
	paths := []string{
		managedDoltStatePath(cityPath),
		providerManagedDoltStatePath(cityPath),
		filepath.Join(cityPath, ".beads", "dolt-server.port"),
	}
	for _, path := range paths {
		if strings.HasSuffix(path, ".json") {
			state, err := readDoltRuntimeStateFile(path)
			if err == nil && state.Port > 0 {
				return strconv.Itoa(state.Port)
			}
			continue
		}
		if port, ok, err := readTrimmedFile(path); err == nil && ok && port != "" {
			return port
		}
	}
	return ""
}

func firstLocalDoltDatabaseDir(doltDir string) (string, bool, error) {
	entries, err := os.ReadDir(doltDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dbPath := filepath.Join(doltDir, entry.Name())
		if isDir(filepath.Join(dbPath, ".dolt")) {
			return dbPath, true, nil
		}
	}
	return "", false, nil
}

func doltSQLServerPIDForDataDir(doltDir string) (int, error) {
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	doltDir = normalizePathForCompare(doltDir)
	for _, entry := range procEntries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		procDir := filepath.Join("/proc", entry.Name())
		cmdline, _ := os.ReadFile(filepath.Join(procDir, "cmdline"))
		cmd := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !strings.Contains(cmd, "dolt") || !strings.Contains(cmd, "sql-server") {
			continue
		}
		if strings.Contains(normalizePathForCompare(cmd), doltDir) {
			return pid, nil
		}
		cwd, err := os.Readlink(filepath.Join(procDir, "cwd"))
		if err != nil {
			continue
		}
		cwd = strings.TrimSuffix(cwd, " (deleted)")
		if samePath(cwd, doltDir) {
			return pid, nil
		}
	}
	return 0, nil
}

func readTrimmedFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
