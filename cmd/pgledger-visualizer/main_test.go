package main

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func flagNames(fs *flag.FlagSet) []string {
	var names []string
	fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
	return names
}

func getenv(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestServeFlagsFromEnv(t *testing.T) {
	var defaults serveOptions
	require.NoError(t, parse(serveFlags(&defaults), nil, getenv(nil)))
	assert.Equal(t, serveOptions{addr: "127.0.0.1:8080", slowMS: -1}, defaults)

	env := map[string]string{
		"DATABASE_URL":                            "postgres://env",
		"PGLEDGER_VISUALIZER_ADDR":                "127.0.0.1:9000",
		"PGLEDGER_VISUALIZER_LOG_SLOW_QUERIES_MS": "5",
		"PGLEDGER_VISUALIZER_DEV":                 "true",
	}
	var o serveOptions
	fs := serveFlags(&o)
	assert.ElementsMatch(t, []string{"database-url", "addr", "log-slow-queries-ms", "dev"}, flagNames(fs))
	require.NoError(t, parse(fs, nil, getenv(env)))
	assert.Equal(t, serveOptions{databaseURL: "postgres://env", addr: "127.0.0.1:9000", slowMS: 5, dev: true}, o)

	o = serveOptions{}
	require.NoError(t, parse(serveFlags(&o), []string{"--addr", "127.0.0.1:9001", "--database-url", "postgres://flag"}, getenv(env)))
	assert.Equal(t, serveOptions{databaseURL: "postgres://flag", addr: "127.0.0.1:9001", slowMS: 5, dev: true}, o)
}

func TestSeedFlagsFromEnv(t *testing.T) {
	var defaults seedOptions
	require.NoError(t, parse(seedFlags(&defaults), nil, getenv(nil)))
	assert.Equal(t, seedOptions{users: 20, seed: 1}, defaults)

	env := map[string]string{
		"DATABASE_URL":                       "postgres://env",
		"PGLEDGER_VISUALIZER_INSTALL_SCHEMA": "true",
		"PGLEDGER_VISUALIZER_USERS":          "3",
		"PGLEDGER_VISUALIZER_SEED":           "9",
	}
	var o seedOptions
	fs := seedFlags(&o)
	assert.ElementsMatch(t, []string{"database-url", "install-schema", "users", "seed"}, flagNames(fs))
	require.NoError(t, parse(fs, nil, getenv(env)))
	assert.Equal(t, seedOptions{databaseURL: "postgres://env", installSchema: true, users: 3, seed: 9}, o)

	o = seedOptions{}
	require.NoError(t, parse(seedFlags(&o), []string{"--users", "4"}, getenv(env)))
	assert.Equal(t, seedOptions{databaseURL: "postgres://env", installSchema: true, users: 4, seed: 9}, o)
}

func TestInvalidEnvValue(t *testing.T) {
	var o seedOptions
	err := parse(seedFlags(&o), nil, getenv(map[string]string{"PGLEDGER_VISUALIZER_USERS": "many"}))
	assert.ErrorContains(t, err, `invalid PGLEDGER_VISUALIZER_USERS "many"`)
}
