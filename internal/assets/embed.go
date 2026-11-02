package assets

import _ "embed"

//go:embed asr_test.wav
var ASRTestWAV []byte

// VLMTestPNG is the sample image sent to a vision-language model when the
// caller uploads no file. The model catalog's "Test" button has no file
// input, so without this fallback every VLLM test failed with
// "image file is required" and the endpoint could never be exercised.
// The image carries legible text, so an OCR backend returns non-empty text
// and the round-trip is visible in the debug result.
//
//go:embed vlm_test.png
var VLMTestPNG []byte
