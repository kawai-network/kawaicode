package tools

import (
	"encoding/base64"
	"fmt"

	"github.com/getkawai/unillm"
)

const toolMediaMetadataKind = "media"

type toolMediaMetadata struct {
	Kind      string `json:"kind"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Text      string `json:"text,omitempty"`
}

func newMediaToolResponse(text, mediaType string, data []byte) unillm.ToolResponse {
	encoded := base64.StdEncoding.EncodeToString(data)
	return newMediaToolResponseFromBase64(text, mediaType, encoded)
}

func newMediaToolResponseFromBase64(text, mediaType, encoded string) unillm.ToolResponse {
	if text == "" {
		text = fmt.Sprintf("Loaded %s content", mediaType)
	}
	resp := unillm.NewTextResponse(text)
	return unillm.WithResponseMetadata(resp, toolMediaMetadata{
		Kind:      toolMediaMetadataKind,
		MediaType: mediaType,
		Data:      encoded,
		Text:      text,
	})
}
