package aigateway

import (
	"context"
	"net/http"
	"time"
)

// ImagesGeneratePath is the gateway endpoint for image generation.
const ImagesGeneratePath = "/v1/images/generations"

// ImageGenerateRequest is the body of POST /v1/images/generations.
//
// Mirrors OpenAI's images.generations schema. AOSentry routes the request
// to the underlying provider based on the model id.
type ImageGenerateRequest struct {
	Prompt  string `json:"prompt"`
	Model   string `json:"model,omitempty"`   // e.g. "dall-e-3"
	Size    string `json:"size,omitempty"`    // e.g. "1024x1024"
	Quality string `json:"quality,omitempty"` // "standard" | "hd"
	Style   string `json:"style,omitempty"`   // "vivid" | "natural"
	N       int    `json:"n,omitempty"`       // number of images, default 1
}

// ImageData is one entry in an ImageGenerateResponse. Either URL or B64 is
// populated depending on the gateway's configured response_format.
type ImageData struct {
	URL string `json:"url,omitempty"`
	B64 string `json:"b64_json,omitempty"`
}

// ImageGenerateResponse holds the generated image(s).
type ImageGenerateResponse struct {
	Data  []ImageData `json:"data"`
	Model string      `json:"model"`
	Usage Usage       `json:"usage,omitempty"`
}

// GenerateImage requests one or more generated images. If req.N is 0 it
// defaults to 1.
func (c *Client) GenerateImage(ctx context.Context, req ImageGenerateRequest) (*ImageGenerateResponse, error) {
	if req.N == 0 {
		req.N = 1
	}
	started := time.Now()
	var resp ImageGenerateResponse
	err := c.doJSON(ctx, http.MethodPost, ImagesGeneratePath, req, &resp)
	c.observe(ctx, OpImageGenerate, resp.Model, resp.Usage, started, err)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
