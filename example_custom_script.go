package progjpeg

import (
	"bytes"
	"image"
	"image/color"
)

// ExampleCustomScanScript demonstrates how to use custom progressive scan scripts.
func ExampleCustomScanScript() {
	// Create a simple test image
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 128, 255})
		}
	}

	// Example 1: Using default progressive encoding
	var buf1 bytes.Buffer
	_ = Encode(&buf1, img, &Options{
		Quality:     80,
		Progressive: true,
		// ScanScript is nil, so default script will be used
	})

	// Example 2: Using the fast loading scan script
	var buf2 bytes.Buffer
	_ = Encode(&buf2, img, &Options{
		Quality:     80,
		Progressive: true,
		ScanScript:  DefaultColorScanScript(),
	})

	// Example 3: Creating a custom scan script
	customScript := ScanScript{
		// Start with DC for quick preview
		{Component: -1, SpectralStart: 0, SpectralEnd: 0},
		// Add minimal AC for basic structure (Y only first)
		{Component: 0, SpectralStart: 1, SpectralEnd: 3},
		// Add color information
		{Component: 1, SpectralStart: 1, SpectralEnd: 3},
		{Component: 2, SpectralStart: 1, SpectralEnd: 3},
		// Complete with remaining frequencies
		{Component: 0, SpectralStart: 4, SpectralEnd: 63},
		{Component: 1, SpectralStart: 4, SpectralEnd: 63},
		{Component: 2, SpectralStart: 4, SpectralEnd: 63},
	}

	var buf3 bytes.Buffer
	_ = Encode(&buf3, img, &Options{
		Quality:     80,
		Progressive: true,
		ScanScript:  customScript,
	})

	// Example 4: Grayscale-optimized script
	grayImg := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			grayImg.Set(x, y, color.Gray{uint8((x + y) * 2)})
		}
	}

	grayScript := ScanScript{
		// DC
		{Component: 0, SpectralStart: 0, SpectralEnd: 0},
		// Very low frequencies for quick recognition
		{Component: 0, SpectralStart: 1, SpectralEnd: 3},
		// Low frequencies
		{Component: 0, SpectralStart: 4, SpectralEnd: 15},
		// High frequencies
		{Component: 0, SpectralStart: 16, SpectralEnd: 63},
	}

	var buf4 bytes.Buffer
	_ = Encode(&buf4, grayImg, &Options{
		Quality:     80,
		Progressive: true,
		ScanScript:  grayScript,
	})
}
