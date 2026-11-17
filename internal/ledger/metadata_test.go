package ledger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgr0ss/pgledger-visualizer/internal/ledger"
)

func TestParseMetadataObject(t *testing.T) {
	md := ledger.ParseMetadata([]byte(`{"b":1,"a":"x","c":null,"d":{"e":1}}`))
	assert.Equal(t, []ledger.MetaField{
		{Key: "a", Value: "x", Traceable: true},
		{Key: "b", Value: "1", Traceable: true},
		{Key: "c", Value: "null", Traceable: false},
		{Key: "d", Value: `{"e":1}`, Traceable: false},
	}, md.Fields)
}

func TestParseMetadataNonObject(t *testing.T) {
	md := ledger.ParseMetadata([]byte(`[1, 2]`))
	assert.Equal(t, ledger.Metadata{Raw: "[1,2]"}, md)
}

func TestParseMetadataNil(t *testing.T) {
	assert.Equal(t, ledger.Metadata{}, ledger.ParseMetadata(nil))
}
