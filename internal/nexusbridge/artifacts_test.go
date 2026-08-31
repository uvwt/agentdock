package nexusbridge

import (
	"encoding/base64"
	"testing"

	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

func TestReadArtifactChunkServesPrivateBridgePayload(t *testing.T) {
	store := publicartifacts.New(t.TempDir(), "", 0)
	published, err := store.PublishBytes(publicartifacts.PublishBytesRequest{Filename: "result.txt", Data: []byte("bridge payload")})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{artifacts: store}
	result, err := client.readArtifactChunk(published.ArtifactID, 0, protocol.MaxArtifactChunkBytes)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := result["data_base64"].(string)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(data) != "bridge payload" || result["eof"] != true {
		t.Fatalf("Bridge Artifact result = %#v decoded=%q err=%v", result, data, err)
	}
}
