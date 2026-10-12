package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	_ "github.com/ararat-network/ark/app"
)

var (
	// sourceRow matches an inventory row: the backticked source, its kind, and the fields cell.
	sourceRow = regexp.MustCompile("^\\| `([A-Za-z0-9_./]+)` \\| (query|event) \\| ([^|]+) \\|")
	fieldPath = regexp.MustCompile("`([a-z0-9_.\\[\\]]+)`")
)

// Every query, event, and field protocol monitoring's source inventory lists resolves against the registered protobuf
// descriptors, so a rename or removal of anything an alert reads fails here.
func TestProtocolSourceInventoryResolves(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "docs", "operations", "PROTOCOL_MONITORING.md"))
	require.NoError(t, err)
	start := strings.Index(string(doc), "\n## 8. Source inventory\n")
	require.NotEqual(t, -1, start, "protocol monitoring has no source inventory")

	rows := 0
	for _, line := range strings.Split(string(doc[start:]), "\n") {
		m := sourceRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rows++
		source, kind := m[1], m[2]
		t.Run(source, func(t *testing.T) {
			msg := resolveSource(t, source, kind)
			fields := fieldPath.FindAllStringSubmatch(m[3], -1)
			require.NotEmpty(t, fields, "row lists no fields")
			for _, f := range fields {
				requireFieldPath(t, msg, f[1])
			}
		})
	}
	require.Positive(t, rows, "the source inventory lists no sources")
}

// resolveSource returns the message a query answers with or an event carries.
func resolveSource(t *testing.T, source, kind string) protoreflect.MessageDescriptor {
	t.Helper()
	if kind == "event" {
		desc, err := gogoproto.HybridResolver.FindDescriptorByName(protoreflect.FullName(source))
		require.NoError(t, err)
		msg, ok := desc.(protoreflect.MessageDescriptor)
		require.True(t, ok, "%s is not a message", source)
		return msg
	}
	service, method, ok := strings.Cut(source, "/")
	require.True(t, ok, "query %s names no method", source)
	desc, err := gogoproto.HybridResolver.FindDescriptorByName(protoreflect.FullName(service))
	require.NoError(t, err)
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	require.True(t, ok, "%s is not a service", service)
	rpc := svc.Methods().ByName(protoreflect.Name(method))
	require.NotNil(t, rpc, "%s has no method %s", service, method)
	return rpc.Output()
}

// requireFieldPath walks a dotted path, with [] marking a repeated field, down from msg.
func requireFieldPath(t *testing.T, msg protoreflect.MessageDescriptor, path string) {
	t.Helper()
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		repeated := strings.HasSuffix(segment, "[]")
		name := strings.TrimSuffix(segment, "[]")
		field := msg.Fields().ByName(protoreflect.Name(name))
		require.NotNil(t, field, "%s has no field %s (path %s)", msg.FullName(), name, path)
		require.Equal(t, repeated, field.IsList(), "%s.%s repetition is marked wrongly in %s", msg.FullName(), name, path)
		if i < len(segments)-1 {
			msg = field.Message()
			require.NotNil(t, msg, "%s.%s is not a message, so %s cannot descend", msg, name, path)
		}
	}
}
