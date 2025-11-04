# progjpeg

A Go package for progressive JPEG encoding with custom scan script support (and default scan scripts, of course).

Why? You like JPEG (webp is too recent for you), but sometimes the network is slow and you prefer a fast, blurry preview to nothing. Encode images like it's 1999 !!1!

This package includes large portions of source code derived and/or copied from the Go standard library (image/jpeg), licensed under the BSD 3-Clause License.

Spectral selection is implemented (going from low frequencies to high frequencies), but not successive approximations (going from most significant bits to least significant bits).

## Disclaimer

This development started around 2018-2019, 95% of the work was done, it was relatively easy (the hard work had been done by the Go team already, it was just a matter of reading/deciphering JPEG/JFIF reference material and stdlib code ;) but I was stuck for a long time on some bugs, that Claude Sonnet 4 corrected (I'm so grateful!):

1. an off-by-one error in the spectral loop in writePartialBlock (zig <= se)
2. missing flush & reset of the bit buffer at the end of a progressive scan, in writeProgressiveSOS

Additionally, AI did a refactoring (dedup) of writeSOS/writePartialSOS, and the custom scan script support (I was going to release with default scripts only, but it wasn't complicated, just tedious, to support custom scripts), including most of the documentation.

## Usage

```go
package main

import (
    "os"
    "image/png"
    "github.com/dlecorfec/progjpeg"
)

func main() {
    // Read input image
    file, _ := os.Open("input.png")
    defer file.Close()
    img, _ := png.Decode(file)
    
    // Create output file
    output, _ := os.Create("output.jpg")
    defer output.Close()
    
    // Encode as progressive JPEG
    err := progjpeg.Encode(output, img, &progjpeg.Options{
        Quality:     80,
        Progressive: true,
    })
    if err != nil {
        panic(err)
    }
}
```

## Scan scripts

### Overview

Progressive JPEG encoding allows images to be displayed incrementally as they load. The custom scan script feature lets you control exactly how this progressive loading happens by specifying which DCT coefficients are encoded in each scan.

DCT coefficients go from 0 to 63, 0 being the lowest frequency and 63 the highest.
Coefficient 0 is called DC, the others are called AC.


### Basic Usage

```go
// Use default progressive encoding
err := progjpeg.Encode(w, img, &progjpeg.Options{
    Quality:     80,
    Progressive: true,
})

// Use a predefined scan script
err := progjpeg.Encode(w, img, &progjpeg.Options{
    Quality:     80,
    Progressive: true,
    ScanScript:  progjpeg.DefaultColorScanScript(),
})

// Use a completely custom scan script
customScript := progjpeg.ScanScript{
    {Component: -1, SpectralStart: 0, SpectralEnd: 0},      // DC scan
    {Component: 0, SpectralStart: 1, SpectralEnd: 5},       // Y low AC
    {Component: 1, SpectralStart: 1, SpectralEnd: 5},       // Cb low AC
    {Component: 2, SpectralStart: 1, SpectralEnd: 5},       // Cr low AC
    {Component: 0, SpectralStart: 6, SpectralEnd: 63},      // Y high AC
    {Component: 1, SpectralStart: 6, SpectralEnd: 63},      // Cb high AC
    {Component: 2, SpectralStart: 6, SpectralEnd: 63},      // Cr high AC
}

err := progjpeg.Encode(w, img, &progjpeg.Options{
    Quality:     80,
    Progressive: true,
    ScanScript:  customScript,
})
```

### Scan Parameters

Each `ProgressiveScan` in a `ScanScript` has these fields:

#### Component
- `-1`: All components (only valid for DC scans)
- `0`: Y (luminance) component
- `1`: Cb (blue chrominance) component  
- `2`: Cr (red chrominance) component

#### SpectralStart, SpectralEnd
- Range: `0-63` (DCT coefficient indices in zigzag order)
- `0,0`: DC coefficient only
- `1,5`: Low frequency AC coefficients
- `6,63`: High frequency AC coefficients
- `1,63`: All AC coefficients

### Predefined Scan Scripts

#### DefaultGrayscaleScanScript()

1. DC scan
2. Low frequency AC (1-9)
3. High frequency AC (10-63)

#### DefaultColorScanScript()

1. DC scan for all components
2. Very low frequency AC for Y only (1-2)
3. Slightly more Y detail (3-9)
4. Add color information (Cb, Cr low frequencies)
5. Complete remaining frequencies

### Validation Rules

Scan scripts are validated to ensure they produce valid JPEG files:

1. **Component ranges**: Must be -1 to (nComponent-1)
2. **Spectral ranges**: 0-63, SpectralEnd >= SpectralStart
3. **DC scan constraints**: Component -1 only valid for SpectralStart=SpectralEnd=0
4. **AC scan constraints**: Component -1 not allowed for AC scans

Invalid scan scripts automatically fall back to default scripts.

### Design Considerations

#### Progressive Loading Strategy
- **DC first**: Always start with DC coefficients for fastest preview
- **Y before Cb/Cr**: Luminance is more visually important than chrominance
- **Low frequencies first**: Low frequency AC coefficients contribute more to perceived image quality

#### Performance vs Quality
- **Fewer scans**: Faster encoding, less progressive benefit
- **More scans**: Better progressive loading, slower encoding
- **Component separation**: Better progressive loading, more overhead

#### Example Strategies

**Fast Preview**: Prioritize getting any recognizable image quickly
```go
ScanScript{
    {Component: -1, SpectralStart: 0, SpectralEnd: 0},    // DC all
    {Component: 0, SpectralStart: 1, SpectralEnd: 2},     // Y minimal AC
    {Component: 0, SpectralStart: 3, SpectralEnd: 63},    // Y remaining
    {Component: 1, SpectralStart: 1, SpectralEnd: 63},    // Cb all AC
    {Component: 2, SpectralStart: 1, SpectralEnd: 63},    // Cr all AC
}
```

**High Quality Progressive**: Maximum progressive steps for smooth loading
```go
ScanScript{
    {Component: -1, SpectralStart: 0, SpectralEnd: 0},     // DC all
    {Component: 0, SpectralStart: 1, SpectralEnd: 1},      // Y AC 1
    {Component: 0, SpectralStart: 2, SpectralEnd: 3},      // Y AC 2-3
    {Component: 0, SpectralStart: 4, SpectralEnd: 7},      // Y AC 4-7
    {Component: 1, SpectralStart: 1, SpectralEnd: 3},      // Cb AC 1-3
    {Component: 2, SpectralStart: 1, SpectralEnd: 3},      // Cr AC 1-3
    {Component: 0, SpectralStart: 8, SpectralEnd: 63},     // Y AC remaining
    {Component: 1, SpectralStart: 4, SpectralEnd: 63},     // Cb AC remaining
    {Component: 2, SpectralStart: 4, SpectralEnd: 63},     // Cr AC remaining
}
```