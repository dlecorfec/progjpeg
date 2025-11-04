// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package progjpeg

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
)

// div returns a/b rounded to the nearest integer, instead of rounded to zero.
func div(a, b int32) int32 {
	if a >= 0 {
		return (a + (b >> 1)) / b
	}
	return -((-a + (b >> 1)) / b)
}

// bitCount counts the number of bits needed to hold an integer.
var bitCount = [256]byte{
	0, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

type quantIndex int

const (
	quantIndexLuminance quantIndex = iota
	quantIndexChrominance
	nQuantIndex
)

// unscaledQuant are the unscaled quantization tables in zig-zag order. Each
// encoder copies and scales the tables according to its quality parameter.
// The values are derived from section K.1 of the spec, after converting from
// natural to zig-zag order.
var unscaledQuant = [nQuantIndex][blockSize]byte{
	// Luminance.
	{
		16, 11, 12, 14, 12, 10, 16, 14,
		13, 14, 18, 17, 16, 19, 24, 40,
		26, 24, 22, 22, 24, 49, 35, 37,
		29, 40, 58, 51, 61, 60, 57, 51,
		56, 55, 64, 72, 92, 78, 64, 68,
		87, 69, 55, 56, 80, 109, 81, 87,
		95, 98, 103, 104, 103, 62, 77, 113,
		121, 112, 100, 120, 92, 101, 103, 99,
	},
	// Chrominance.
	{
		17, 18, 18, 24, 21, 24, 47, 26,
		26, 47, 99, 66, 56, 66, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
	},
}

type huffIndex int

const (
	huffIndexLuminanceDC huffIndex = iota
	huffIndexLuminanceAC
	huffIndexChrominanceDC
	huffIndexChrominanceAC
	nHuffIndex
)

// huffmanSpec specifies a Huffman encoding.
type huffmanSpec struct {
	// count[i] is the number of codes of length i+1 bits.
	count [16]byte
	// value[i] is the decoded value of the i'th codeword.
	value []byte
}

// theHuffmanSpec is the Huffman encoding specifications.
//
// This encoder uses the same Huffman encoding for all images. It is also the
// same Huffman encoding used by section K.3 of the spec.
//
// The DC tables have 12 decoded values, called categories.
//
// The AC tables have 162 decoded values: bytes that pack a 4-bit Run and a
// 4-bit Size. There are 16 valid Runs and 10 valid Sizes, plus two special R|S
// cases: 0|0 (meaning EOB) and F|0 (meaning ZRL).
var theHuffmanSpec = [nHuffIndex]huffmanSpec{
	// Luminance DC.
	{
		[16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	// Luminance AC.
	{
		[16]byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 125},
		[]byte{
			0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
			0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
			0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08,
			0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
			0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16,
			0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
			0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
			0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
			0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
			0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
			0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79,
			0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
			0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
			0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
			0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6,
			0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
			0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4,
			0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
			0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea,
			0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
	// Chrominance DC.
	{
		[16]byte{0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	// Chrominance AC.
	{
		[16]byte{0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 119},
		[]byte{
			0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
			0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
			0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91,
			0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
			0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34,
			0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
			0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38,
			0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
			0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
			0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
			0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96,
			0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
			0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4,
			0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
			0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2,
			0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
			0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9,
			0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
}

// huffmanLUT is a compiled look-up table representation of a huffmanSpec.
// Each value maps to a uint32 of which the 8 most significant bits hold the
// codeword size in bits and the 24 least significant bits hold the codeword.
// The maximum codeword size is 16 bits.
type huffmanLUT []uint32

func (h *huffmanLUT) init(s huffmanSpec) {
	maxValue := 0
	for _, v := range s.value {
		if int(v) > maxValue {
			maxValue = int(v)
		}
	}
	*h = make([]uint32, maxValue+1)
	code, k := uint32(0), 0
	for i := 0; i < len(s.count); i++ {
		nBits := uint32(i+1) << 24
		for j := uint8(0); j < s.count[i]; j++ {
			(*h)[s.value[k]] = nBits | code
			code++
			k++
		}
		code <<= 1
	}
}

// theHuffmanLUT are compiled representations of theHuffmanSpec.
var theHuffmanLUT [4]huffmanLUT

func init() {
	for i, s := range theHuffmanSpec {
		theHuffmanLUT[i].init(s)
	}
}

// writer is a buffered writer.
type writer interface {
	Flush() error
	io.Writer
	io.ByteWriter
}

// encoder encodes an image to the JPEG format.
type encoder struct {
	// w is the writer to write to. err is the first error encountered during
	// writing. All attempted writes after the first error become no-ops.
	w   writer
	err error
	// buf is a scratch buffer.
	buf [16]byte
	// bits and nBits are accumulated bits to write to w.
	bits, nBits uint32
	// quant is the scaled quantization tables, in zig-zag order.
	quant [nQuantIndex][blockSize]byte
}

func (e *encoder) flush() {
	if e.err != nil {
		return
	}
	e.err = e.w.Flush()
}

func (e *encoder) write(p []byte) {
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(p)
}

func (e *encoder) writeByte(b byte) {
	if e.err != nil {
		return
	}
	e.err = e.w.WriteByte(b)
}

// emit emits the least significant nBits bits of bits to the bit-stream.
// The precondition is bits < 1<<nBits && nBits <= 16.
func (e *encoder) emit(bits, nBits uint32) {
	nBits += e.nBits
	bits <<= 32 - nBits
	bits |= e.bits
	for nBits >= 8 {
		b := uint8(bits >> 24)
		e.writeByte(b)
		if b == 0xff {
			e.writeByte(0x00)
		}
		bits <<= 8
		nBits -= 8
	}
	e.bits, e.nBits = bits, nBits
}

// emitHuff emits the given value with the given Huffman encoder.
func (e *encoder) emitHuff(h huffIndex, value int32) {
	x := theHuffmanLUT[h][value]
	e.emit(x&(1<<24-1), x>>24)
}

// emitHuffRLE emits a run of runLength copies of value encoded with the given
// Huffman encoder.
func (e *encoder) emitHuffRLE(h huffIndex, runLength, value int32) {
	a, b := value, value
	if a < 0 {
		a, b = -value, value-1
	}
	var nBits uint32
	if a < 0x100 {
		nBits = uint32(bitCount[a])
	} else {
		nBits = 8 + uint32(bitCount[a>>8])
	}
	e.emitHuff(h, runLength<<4|int32(nBits))
	if nBits > 0 {
		e.emit(uint32(b)&(1<<nBits-1), nBits)
	}
}

// writeMarkerHeader writes the header for a marker with the given length.
func (e *encoder) writeMarkerHeader(marker uint8, markerlen int) {
	e.buf[0] = 0xff
	e.buf[1] = marker
	e.buf[2] = uint8(markerlen >> 8)
	e.buf[3] = uint8(markerlen & 0xff)
	e.write(e.buf[:4])
}

// writeDQT writes the Define Quantization Table marker.
func (e *encoder) writeDQT() {
	const markerlen = 2 + int(nQuantIndex)*(1+blockSize)
	e.writeMarkerHeader(dqtMarker, markerlen)
	for i := range e.quant {
		e.writeByte(uint8(i))
		e.write(e.quant[i][:])
	}
}

// writeSOF0 writes the Start Of Frame (Baseline Sequential) marker.
func (e *encoder) writeSOF(size image.Point, nComponent int, marker uint8) {
	markerlen := 8 + 3*nComponent
	e.writeMarkerHeader(marker, markerlen)
	e.buf[0] = 8 // 8-bit color.
	e.buf[1] = uint8(size.Y >> 8)
	e.buf[2] = uint8(size.Y & 0xff)
	e.buf[3] = uint8(size.X >> 8)
	e.buf[4] = uint8(size.X & 0xff)
	e.buf[5] = uint8(nComponent)
	if nComponent == 1 {
		e.buf[6] = 1
		// No subsampling for grayscale image.
		e.buf[7] = 0x11
		e.buf[8] = 0x00
	} else {
		for i := 0; i < nComponent; i++ {
			e.buf[3*i+6] = uint8(i + 1)
			// We use 4:2:0 chroma subsampling.
			e.buf[3*i+7] = "\x22\x11\x11"[i]
			e.buf[3*i+8] = "\x00\x01\x01"[i]
		}
	}
	e.write(e.buf[:3*(nComponent-1)+9])
}

// writeDHT writes the Define Huffman Table marker.
func (e *encoder) writeDHT(nComponent int) {
	markerlen := 2
	specs := theHuffmanSpec[:]
	if nComponent == 1 {
		// Drop the Chrominance tables.
		specs = specs[:2]
	}
	for _, s := range specs {
		markerlen += 1 + 16 + len(s.value)
	}
	e.writeMarkerHeader(dhtMarker, markerlen)
	for i, s := range specs {
		e.writeByte("\x00\x10\x01\x11"[i])
		e.write(s.count[:])
		e.write(s.value)
	}
}

// writeBlock writes a block of pixel data using the given quantization table,
// returning the post-quantized DC value of the DCT-transformed block. b is in
// natural (not zig-zag) order.
func (e *encoder) writeBlock(b *block, q quantIndex, prevDC int32) int32 {
	fdct(b)
	// Emit the DC delta.
	dc := div(b[0], 8*int32(e.quant[q][0]))
	e.emitHuffRLE(huffIndex(2*q+0), 0, dc-prevDC)
	// Emit the AC components.
	h, runLength := huffIndex(2*q+1), int32(0)
	for zig := 1; zig < blockSize; zig++ {
		ac := div(b[unzig[zig]], 8*int32(e.quant[q][zig]))
		if ac == 0 {
			runLength++
		} else {
			for runLength > 15 {
				e.emitHuff(h, 0xf0)
				runLength -= 16
			}
			e.emitHuffRLE(h, runLength, ac)
			runLength = 0
		}
	}
	if runLength > 0 {
		e.emitHuff(h, 0x00)
	}
	return dc
}

// toYCbCr converts the 8x8 region of m whose top-left corner is p to its
// YCbCr values.
func toYCbCr(m image.Image, p image.Point, yBlock, cbBlock, crBlock *block) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			r, g, b, _ := m.At(min(p.X+i, xmax), min(p.Y+j, ymax)).RGBA()
			yy, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(b>>8))
			yBlock[8*j+i] = int32(yy)
			cbBlock[8*j+i] = int32(cb)
			crBlock[8*j+i] = int32(cr)
		}
	}
}

// grayToY stores the 8x8 region of m whose top-left corner is p in yBlock.
func grayToY(m *image.Gray, p image.Point, yBlock *block) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	pix := m.Pix
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			idx := m.PixOffset(min(p.X+i, xmax), min(p.Y+j, ymax))
			yBlock[8*j+i] = int32(pix[idx])
		}
	}
}

// rgbaToYCbCr is a specialized version of toYCbCr for image.RGBA images.
func rgbaToYCbCr(m *image.RGBA, p image.Point, yBlock, cbBlock, crBlock *block) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	for j := 0; j < 8; j++ {
		sj := p.Y + j
		if sj > ymax {
			sj = ymax
		}
		offset := (sj-b.Min.Y)*m.Stride - b.Min.X*4
		for i := 0; i < 8; i++ {
			sx := p.X + i
			if sx > xmax {
				sx = xmax
			}
			pix := m.Pix[offset+sx*4:]
			yy, cb, cr := color.RGBToYCbCr(pix[0], pix[1], pix[2])
			yBlock[8*j+i] = int32(yy)
			cbBlock[8*j+i] = int32(cb)
			crBlock[8*j+i] = int32(cr)
		}
	}
}

// yCbCrToYCbCr is a specialized version of toYCbCr for image.YCbCr images.
func yCbCrToYCbCr(m *image.YCbCr, p image.Point, yBlock, cbBlock, crBlock *block) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	for j := 0; j < 8; j++ {
		sy := p.Y + j
		if sy > ymax {
			sy = ymax
		}
		for i := 0; i < 8; i++ {
			sx := p.X + i
			if sx > xmax {
				sx = xmax
			}
			yi := m.YOffset(sx, sy)
			ci := m.COffset(sx, sy)
			yBlock[8*j+i] = int32(m.Y[yi])
			cbBlock[8*j+i] = int32(m.Cb[ci])
			crBlock[8*j+i] = int32(m.Cr[ci])
		}
	}
}

// scale scales the 16x16 region represented by the 4 src blocks to the 8x8
// dst block.
func scale(dst *block, src *[4]block) {
	for i := 0; i < 4; i++ {
		dstOff := (i&2)<<4 | (i&1)<<2
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				j := 16*y + 2*x
				sum := src[i][j] + src[i][j+1] + src[i][j+8] + src[i][j+9]
				dst[8*y+x+dstOff] = (sum + 2) >> 2
			}
		}
	}
}

// sosHeaderY is the SOS marker "\xff\xda" followed by 8 bytes:
//   - the marker length "\x00\x08",
//   - the number of components "\x01",
//   - component 1 uses DC table 0 and AC table 0 "\x01\x00",
//   - the bytes "\x00\x3f\x00". Section B.2.3 of the spec says that for
//     sequential DCTs, those bytes (8-bit Ss, 8-bit Se, 4-bit Ah, 4-bit Al)
//     should be 0x00, 0x3f, 0x00<<4 | 0x00.
var sosHeaderY = []byte{
	0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00,
}

// sosHeaderYCbCr is the SOS marker "\xff\xda" followed by 12 bytes:
//   - the marker length "\x00\x0c",
//   - the number of components "\x03",
//   - component 1 uses DC table 0 and AC table 0 "\x01\x00",
//   - component 2 uses DC table 1 and AC table 1 "\x02\x11",
//   - component 3 uses DC table 1 and AC table 1 "\x03\x11",
//   - the bytes "\x00\x3f\x00". Section B.2.3 of the spec says that for
//     sequential DCTs, those bytes (8-bit Ss, 8-bit Se, 4-bit Ah, 4-bit Al)
//     should be 0x00, 0x3f, 0x00<<4 | 0x00.
var sosHeaderYCbCr = []byte{
	0xff, 0xda, 0x00, 0x0c, 0x03, 0x01, 0x00, 0x02,
	0x11, 0x03, 0x11, 0x00, 0x3f, 0x00,
}

// writeSOS writes the StartOfScan marker.
func (e *encoder) writeSOS(m image.Image) {
	switch m.(type) {
	case *image.Gray:
		e.write(sosHeaderY)
	default:
		e.write(sosHeaderYCbCr)
	}

	// Process all blocks using baseline encoding
	e.processImageBlocks(m, -1, e.writeBlock)

	// Pad the last byte with 1's.
	e.emit(0x7f, 7)
}

// blockProcessor defines a function that processes a block of DCT coefficients.
// It receives the block, quantization index, previous DC value, and returns the new DC value.
type blockProcessor func(b *block, q quantIndex, prevDC int32) int32

// processImageBlocks iterates over image blocks and calls the processor function for each block.
// This function consolidates the common block iteration logic used by both baseline and progressive encoding.
func (e *encoder) processImageBlocks(m image.Image, component int, processor blockProcessor) {
	var (
		// Scratch buffers to hold the YCbCr values.
		// The blocks are in natural (not zig-zag) order.
		b      block
		cb, cr [4]block
		// DC components are delta-encoded.
		prevDCY, prevDCCb, prevDCCr int32
	)
	bounds := m.Bounds()

	switch m := m.(type) {
	case *image.Gray:
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 8 {
			for x := bounds.Min.X; x < bounds.Max.X; x += 8 {
				p := image.Pt(x, y)
				grayToY(m, p, &b)
				prevDCY = processor(&b, 0, prevDCY)
			}
		}
	default:
		rgba, _ := m.(*image.RGBA)
		ycbcr, _ := m.(*image.YCbCr)

		if component != 0 {
			// Process color image with potential component filtering
			for y := bounds.Min.Y; y < bounds.Max.Y; y += 16 {
				for x := bounds.Min.X; x < bounds.Max.X; x += 16 {
					for i := 0; i < 4; i++ {
						xOff := (i & 1) * 8 // 0 8 0 8
						yOff := (i & 2) * 4 // 0 0 8 8
						p := image.Pt(x+xOff, y+yOff)
						if rgba != nil {
							rgbaToYCbCr(rgba, p, &b, &cb[i], &cr[i])
						} else if ycbcr != nil {
							yCbCrToYCbCr(ycbcr, p, &b, &cb[i], &cr[i])
						} else {
							toYCbCr(m, p, &b, &cb[i], &cr[i])
						}
						if component == -1 || component == 0 {
							prevDCY = processor(&b, 0, prevDCY)
						}
					}
					if component == -1 || component == 1 {
						scale(&b, &cb)
						prevDCCb = processor(&b, 1, prevDCCb)
					}
					if component == -1 || component == 2 {
						scale(&b, &cr)
						prevDCCr = processor(&b, 1, prevDCCr)
					}
				}
			}
		} else {
			// Y component only processing
			for y := bounds.Min.Y; y < bounds.Max.Y; y += 8 {
				for x := bounds.Min.X; x < bounds.Max.X; x += 8 {
					p := image.Pt(x, y)
					if rgba != nil {
						rgbaToYCbCr(rgba, p, &b, &cb[0], &cr[0])
					} else if ycbcr != nil {
						yCbCrToYCbCr(ycbcr, p, &b, &cb[0], &cr[0])
					} else {
						toYCbCr(m, p, &b, &cb[0], &cr[0])
					}
					prevDCY = processor(&b, 0, prevDCY)
				}
			}
		}
	}
}

// DefaultQuality is the default quality encoding parameter.
const DefaultQuality = 75

// ProgressiveScan represents a single scan in a progressive JPEG sequence.
// Each scan encodes a specific subset of the DCT coefficients.
type ProgressiveScan struct {
	// Component specifies which color component to encode:
	// -1 = all components (DC scan), 0 = Y (luminance), 1 = Cb, 2 = Cr
	Component int

	// SpectralStart and SpectralEnd define the range of DCT coefficients (0-63)
	// 0,0 = DC only, 1,5 = low frequency AC, 6,63 = high frequency AC
	SpectralStart, SpectralEnd int

	// SuccessiveApproxHigh and SuccessiveApproxLow control bit-plane refinement
	// For spectral selection only: both should be 0
	// For successive approximation: ah=starting bit position, al=ending bit position
	SuccessiveApproxHigh, SuccessiveApproxLow int
}

// ScanScript defines a complete progressive scan sequence.
type ScanScript []ProgressiveScan

// Options are the encoding parameters.
// Quality ranges from 1 to 100 inclusive, higher is better.
type Options struct {
	Quality     int
	Progressive bool

	// ScanScript defines a custom progressive scan sequence.
	// If nil, default scan scripts are used based on the image type.
	// Only used when Progressive is true.
	ScanScript ScanScript
}

// Encode writes the Image m to w in JPEG 4:2:0 baseline format with the given
// options. Default parameters are used if a nil *[Options] is passed.
func Encode(w io.Writer, m image.Image, o *Options) error {
	b := m.Bounds()
	if b.Dx() >= 1<<16 || b.Dy() >= 1<<16 {
		return errors.New("jpeg: image is too large to encode")
	}
	var e encoder
	if ww, ok := w.(writer); ok {
		e.w = ww
	} else {
		e.w = bufio.NewWriter(w)
	}
	// Clip quality to [1, 100].
	quality := DefaultQuality
	if o != nil {
		quality = o.Quality
		if quality < 1 {
			quality = 1
		} else if quality > 100 {
			quality = 100
		}
	}
	// Convert from a quality rating to a scaling factor.
	var scale int
	if quality < 50 {
		scale = 5000 / quality
	} else {
		scale = 200 - quality*2
	}
	// Initialize the quantization tables.
	for i := range e.quant {
		for j := range e.quant[i] {
			x := int(unscaledQuant[i][j])
			x = (x*scale + 50) / 100
			if x < 1 {
				x = 1
			} else if x > 255 {
				x = 255
			}
			e.quant[i][j] = uint8(x)
		}
	}
	// Compute number of components based on input image type.
	nComponent := 3
	switch m.(type) {
	// TODO(wathiede): switch on m.ColorModel() instead of type.
	case *image.Gray:
		nComponent = 1
	}
	// Write the Start Of Image marker.
	e.buf[0] = 0xff
	e.buf[1] = 0xd8
	e.write(e.buf[:2])
	// Write the quantization tables.
	e.writeDQT()
	if o != nil && o.Progressive {
		e.writeProgressive(m, b, nComponent, o)
	} else {
		// Write the image dimensions.
		e.writeSOF(b.Size(), nComponent, sof0Marker)
		// Write the Huffman tables.
		e.writeDHT(nComponent)
		// Write the image data.
		e.writeSOS(m)
	}
	// Write the End Of Image marker.
	e.buf[0] = 0xff
	e.buf[1] = 0xd9
	e.write(e.buf[:2])
	e.flush()
	return e.err
}

// DefaultGrayscaleScanScript returns the default progressive scan script for grayscale images.
func DefaultGrayscaleScanScript() ScanScript {
	return ScanScript{
		// DC scan
		{Component: 0, SpectralStart: 0, SpectralEnd: 0},
		// Low frequency AC
		{Component: 0, SpectralStart: 1, SpectralEnd: 9},
		// High frequency AC
		{Component: 0, SpectralStart: 10, SpectralEnd: 63},
	}
}

// DefaultColorScanScript returns the default progressive scan script optimized for fast initial display.
// This puts more emphasis on getting a viewable image quickly and is used as the default
// for color images when no custom scan script is specified.
func DefaultColorScanScript() ScanScript {
	return ScanScript{
		// DC scan for all components
		{Component: -1, SpectralStart: 0, SpectralEnd: 0},
		// Very low frequency AC for Y only - fastest recognizable image
		{Component: 0, SpectralStart: 1, SpectralEnd: 2},
		// Slightly more Y detail
		{Component: 0, SpectralStart: 3, SpectralEnd: 9},
		// Add color information
		{Component: 1, SpectralStart: 1, SpectralEnd: 5},
		{Component: 2, SpectralStart: 1, SpectralEnd: 5},
		// Complete the image
		{Component: 0, SpectralStart: 10, SpectralEnd: 63},
		{Component: 1, SpectralStart: 6, SpectralEnd: 63},
		{Component: 2, SpectralStart: 6, SpectralEnd: 63},
	}
}

// validateScanScript checks if a scan script is valid for JPEG encoding.
func validateScanScript(script ScanScript, nComponent int) error {
	if len(script) == 0 {
		return errors.New("jpeg: scan script cannot be empty")
	}

	for i, scan := range script {
		// Validate component
		if scan.Component < -1 || scan.Component >= nComponent {
			return fmt.Errorf("jpeg: scan %d has invalid component %d (must be -1 to %d)", i, scan.Component, nComponent-1)
		}

		// Validate spectral selection
		if scan.SpectralStart < 0 || scan.SpectralStart > 63 {
			return fmt.Errorf("jpeg: scan %d has invalid spectral start %d (must be 0-63)", i, scan.SpectralStart)
		}
		if scan.SpectralEnd < scan.SpectralStart || scan.SpectralEnd > 63 {
			return fmt.Errorf("jpeg: scan %d has invalid spectral end %d (must be %d-63)", i, scan.SpectralEnd, scan.SpectralStart)
		}

		// Validate successive approximation
		if scan.SuccessiveApproxHigh < 0 || scan.SuccessiveApproxHigh > 13 {
			return fmt.Errorf("jpeg: scan %d has invalid successive approximation high %d (must be 0-13)", i, scan.SuccessiveApproxHigh)
		}
		if scan.SuccessiveApproxLow < 0 || scan.SuccessiveApproxLow > 13 {
			return fmt.Errorf("jpeg: scan %d has invalid successive approximation low %d (must be 0-13)", i, scan.SuccessiveApproxLow)
		}
		if scan.SuccessiveApproxLow > scan.SuccessiveApproxHigh {
			return fmt.Errorf("jpeg: scan %d has successive approximation low > high (%d > %d)", i, scan.SuccessiveApproxLow, scan.SuccessiveApproxHigh)
		}

		// Validate DC scan constraints
		if scan.SpectralStart == 0 && scan.SpectralEnd == 0 {
			// DC scan - component -1 is allowed for interleaved DC
			if scan.Component != -1 && scan.Component >= nComponent {
				return fmt.Errorf("jpeg: DC scan %d has invalid component %d", i, scan.Component)
			}
		} else {
			// AC scan - component -1 is not allowed
			if scan.Component == -1 {
				return fmt.Errorf("jpeg: AC scan %d cannot have component -1 (interleaved AC not allowed)", i)
			}
		}
	}

	return nil
}

// writeProgressive encodes the image using progressive JPEG format.
// Progressive JPEG allows the image to be displayed incrementally as it loads.
func (e *encoder) writeProgressive(m image.Image, b image.Rectangle, nComponent int, o *Options) {
	// Write the image dimensions.
	e.writeSOF(b.Size(), nComponent, sof2Marker)
	// Write the Huffman tables.
	e.writeDHT(nComponent)

	// Determine which scan script to use
	var script ScanScript
	if o != nil && o.ScanScript != nil {
		script = o.ScanScript
	} else {
		// Use default scan script based on image type
		if nComponent == 3 {
			script = DefaultColorScanScript()
		} else {
			script = DefaultGrayscaleScanScript()
		}
	}

	// Validate the scan script
	if err := validateScanScript(script, nComponent); err != nil {
		// If validation fails, fall back to default script
		if nComponent == 3 {
			script = DefaultColorScanScript()
		} else {
			script = DefaultGrayscaleScanScript()
		}
	}

	// Execute the scan script
	for _, scan := range script {
		e.writeProgressiveSOS(m, scan.SpectralStart, scan.SpectralEnd,
			scan.SuccessiveApproxHigh, scan.SuccessiveApproxLow, scan.Component)
	}
}

// writeProgressiveSOS writes a Start Of Scan marker for a progressive scan
// and processes the image blocks for that scan.
// zigStart and zigEnd define the range of DCT coefficients to encode.
// ah and al define the successive approximation bit positions (currently supports only 0).
// component specifies which color component to encode (-1 for all components).
func (e *encoder) writeProgressiveSOS(m image.Image, zigStart, zigEnd, ah, al, component int) {
	if component != -1 {
		var sosHeaderYShort = []byte{
			0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00,
		}
		sosHeaderYShort[5] = byte(component + 1)
		if component == 1 || component == 2 {
			sosHeaderYShort[6] = 0x11
		} else {
			sosHeaderYShort[6] = 0x00
		}
		e.write(sosHeaderYShort)
	} else {
		var sosHeaderYCbCrShort = []byte{
			0xff, 0xda, 0x00, 0x0c, 0x03, 0x01, 0x00, 0x02,
			0x11, 0x03, 0x11,
		}
		e.write(sosHeaderYCbCrShort)
	}
	refinement := (byte(ah) << 4) | (byte(al) & 0x0F)

	progressiveScript := []byte{byte(zigStart), byte(zigEnd), refinement}
	e.write(progressiveScript)

	// Create a closure that captures the zigzag range for progressive encoding
	processor := func(b *block, q quantIndex, prevDC int32) int32 {
		return e.writePartialBlock(b, q, prevDC, zigStart, zigEnd)
	}

	// Process blocks using the shared logic
	e.processImageBlocks(m, component, processor)

	// Pad the last byte with 1's.
	e.emit(0x7f, 7)
	// Flush any remaining bits and reset the bit buffer for the next scan.
	// In progressive mode, each scan must end with a byte-aligned boundary.
	if e.nBits > 0 {
		// Pad to byte boundary with 1's. We need to add (8 - nBits) more bits.
		bitsNeeded := 8 - e.nBits
		e.emit((1<<bitsNeeded)-1, bitsNeeded)
	}
	// Reset the bit buffer for the next scan
	e.bits = 0
	e.nBits = 0
}

// writePartialBlock writes a block of pixel data for a progressive scan,
// processing only the specified range of DCT coefficients (from ss to se).
// It returns the post-quantized DC value of the DCT-transformed block.
// b is in natural (not zig-zag) order.
func (e *encoder) writePartialBlock(b *block, q quantIndex, prevDC int32, ss, se int) int32 {
	fdct(b)
	if ss == 0 && se == 0 {
		// Emit the DC delta.
		dc := div(b[0], 8*int32(e.quant[q][0]))
		e.emitHuffRLE(huffIndex(2*q+0), 0, dc-prevDC)
		return dc
	}
	if ss > 0 {
		// Emit the AC components.
		h, runLength := huffIndex(2*q+1), int32(0)
		for zig := ss; zig <= se; zig++ {
			ac := div(b[unzig[zig]], 8*int32(e.quant[q][zig]))
			if ac == 0 {
				runLength++
			} else {
				for runLength > 15 {
					e.emitHuff(h, 0xf0)
					runLength -= 16
				}
				e.emitHuffRLE(h, runLength, ac)
				runLength = 0
			}
		}
		if runLength > 0 {
			e.emitHuff(h, 0x00)
		}
	}
	return 0
}
