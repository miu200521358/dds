/*
Copyright 2017 Luke Granger-Brown

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package dds provides a decoder for the DirectDraw surface format,
// which is compatible with the standard image package.
//
// It should normally be used by importing it with a blank name, which
// will cause it to register itself with the image package:
//
//	import _ "github.com/lukegb/dds"
package dds

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

func init() {
	image.RegisterFormat("dds", "DDS ", Decode, DecodeConfig)
}

func DecodeConfig(r io.Reader) (image.Config, error) {
	h, err := readHeader(r)
	if err != nil {
		return image.Config{}, err
	}

	// set width and height
	c := image.Config{
		Width:  int(h.width),
		Height: int(h.height),
	}

	pf := h.pixelFormat
	hasAlpha := (pf.flags&pfAlphaPixels == pfAlphaPixels) || (pf.flags&pfAlpha == pfAlpha)
	hasRGB := (pf.flags&pfFourCC == pfFourCC) || (pf.flags&pfRGB == pfRGB)
	hasYUV := (pf.flags&pfYUV == pfYUV)
	hasLuminance := (pf.flags&pfLuminance == pfLuminance)
	switch {
	case hasRGB && pf.rgbBitCount == 32:
		c.ColorModel = color.RGBAModel
	case hasRGB && pf.rgbBitCount == 64:
		c.ColorModel = color.RGBA64Model
	case hasYUV && pf.rgbBitCount == 24:
		c.ColorModel = color.YCbCrModel
	case hasLuminance && pf.rgbBitCount == 8:
		c.ColorModel = color.GrayModel
	case hasLuminance && pf.rgbBitCount == 16:
		c.ColorModel = color.Gray16Model
	case hasAlpha && pf.rgbBitCount == 8:
		c.ColorModel = color.AlphaModel
	case hasAlpha && pf.rgbBitCount == 16:
		c.ColorModel = color.AlphaModel
	default:
		return image.Config{}, fmt.Errorf("unrecognized image format: hasAlpha: %v, hasRGB: %v, hasYUV: %v, hasLuminance: %v, pf.flags: %x", hasAlpha, hasRGB, hasYUV, hasLuminance, pf.flags)
	}

	return c, nil
}

type img struct {
	h   header
	buf []byte

	rBit, gBit, bBit, aBit uint

	stride, pitch int
}

func (i *img) ColorModel() color.Model {
	return color.NRGBAModel
}

func (i *img) Bounds() image.Rectangle {
	return image.Rect(0, 0, int(i.h.width), int(i.h.height))
}

func (i *img) At(x, y int) color.Color {
	arrPsn := i.pitch*y + i.stride*x
	d := readBits(i.buf[arrPsn:], i.h.pixelFormat.rgbBitCount)
	r := uint8((d & i.h.pixelFormat.rBitMask) >> i.rBit)
	g := uint8((d & i.h.pixelFormat.gBitMask) >> i.gBit)
	b := uint8((d & i.h.pixelFormat.bBitMask) >> i.bBit)
	a := uint8((d & i.h.pixelFormat.aBitMask) >> i.aBit)
	return color.NRGBA{r, g, b, a}
}

func Decode(r io.Reader) (image.Image, error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}

	if h.pixelFormat.flags&pfFourCC == pfFourCC {
		fourCC := uint32(861165636)
		bytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(bytes, fourCC)

		switch string(bytes) {
		case "DXT1":
			// ファイルから圧縮データを読み込む
			compressedData := make([]byte, h.width*h.height)
			if _, err := io.ReadFull(r, compressedData); err != nil {
				return nil, fmt.Errorf("reading compressed image data: %v", err)
			}

			// DXT1デコード処理
			img, err := decodeDXT1(compressedData, int(h.width), int(h.height))
			if err != nil {
				return nil, fmt.Errorf("decoding DXT1: %v", err)
			}

			return img, nil
		case "DXT2", "DXT3":
			// ファイルから圧縮データを読み込む
			compressedData := make([]byte, h.width*h.height)
			if _, err := io.ReadFull(r, compressedData); err != nil {
				return nil, fmt.Errorf("reading compressed image data: %v", err)
			}

			// DXT3デコード処理
			img, err := decodeDXT3(compressedData, int(h.width), int(h.height))
			if err != nil {
				return nil, fmt.Errorf("decoding DXT3: %v", err)
			}

			return img, nil
		case "DXT4", "DXT5":
			// ファイルから圧縮データを読み込む
			compressedData := make([]byte, h.width*h.height)
			if _, err := io.ReadFull(r, compressedData); err != nil {
				return nil, fmt.Errorf("reading compressed image data: %v", err)
			}

			// DXT5デコード処理
			img, err := decodeDXT5(compressedData, int(h.width), int(h.height))
			if err != nil {
				return nil, fmt.Errorf("decoding DXT5: %v", err)
			}

			return img, nil
		default:
			return nil, fmt.Errorf("unsupported FourCC %q", string(bytes))
		}
	}

	if h.pixelFormat.flags != pfAlphaPixels|pfRGB {
		return nil, fmt.Errorf("unsupported pixel format %x", h.pixelFormat.flags)
	}

	pitch := (h.width*h.pixelFormat.rgbBitCount + 7) / 8
	buf := make([]byte, pitch*h.height)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("reading image: %v", err)
	}
	stride := h.pixelFormat.rgbBitCount / 8

	return &img{
		h:   h,
		buf: buf,

		pitch:  int(pitch),
		stride: int(stride),

		rBit: lowestSetBit(h.pixelFormat.rBitMask),
		gBit: lowestSetBit(h.pixelFormat.gBitMask),
		bBit: lowestSetBit(h.pixelFormat.bBitMask),
		aBit: lowestSetBit(h.pixelFormat.aBitMask),
	}, nil
}

func rgb565ToRGBAColor(c uint16) color.RGBA {
	r := uint8((c >> 11) & 0x1F << 3)
	g := uint8((c >> 5) & 0x3F << 2)
	b := uint8((c & 0x1F) << 3)
	return color.RGBA{r, g, b, 255}
}

func interpolateColors(c0, c1 color.RGBA, w0, w1 int) color.RGBA {
	r := (int(c0.R)*w0 + int(c1.R)*w1) / (w0 + w1)
	g := (int(c0.G)*w0 + int(c1.G)*w1) / (w0 + w1)
	b := (int(c0.B)*w0 + int(c1.B)*w1) / (w0 + w1)
	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

// DXT1 ---------------------------------------------------------------------

// decodeDXT1 decodes a DXT1 compressed byte slice into an RGBA image.
func decodeDXT1(compressed []byte, width, height int) (*image.RGBA, error) {
	decompressed := image.NewRGBA(image.Rect(0, 0, width, height))

	blockWidth := (width + 3) / 4
	blockHeight := (height + 3) / 4

loop:
	for blockY := 0; blockY < blockHeight; blockY++ {
		for blockX := 0; blockX < blockWidth; blockX++ {
			if len(compressed) < (blockY*blockWidth+blockX)*8+8 {
				break loop
			}

			blockOffset := (blockY*blockWidth + blockX) * 8 // Each DXT1 block is 8 bytes
			decodeBlockDXT1(compressed[blockOffset:blockOffset+8], decompressed, blockX*4, blockY*4, width)
		}
	}

	return decompressed, nil
}

func decodeBlockDXT1(block []byte, img *image.RGBA, x, y, width int) {
	c0 := binary.LittleEndian.Uint16(block[0:2])
	c1 := binary.LittleEndian.Uint16(block[2:4])
	colorData := binary.LittleEndian.Uint32(block[4:8])

	colors := make([]color.RGBA, 4)
	colors[0] = rgb565ToRGBAColor(c0)
	colors[1] = rgb565ToRGBAColor(c1)
	if c0 > c1 {
		colors[2] = interpolateColors(colors[0], colors[1], 2, 1)
		colors[3] = interpolateColors(colors[0], colors[1], 1, 2)
	} else {
		colors[2] = interpolateColors(colors[0], colors[1], 1, 1)
		colors[3] = color.RGBA{0, 0, 0, 0}
	}

	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			px := x + i
			py := y + j
			if px >= width {
				continue
			}

			colorIndex := (colorData >> uint((j*4+i)*2)) & 0x3
			color := colors[colorIndex]

			img.Set(px, py, color)
		}
	}
}

// DXT2, DXT3 ---------------------------------------------------------------

// decodeDXT3 decodes a DXT3 (similar to DXT2 but without premultiplied alpha) compressed byte slice into an RGBA image.
func decodeDXT3(compressed []byte, width, height int) (*image.RGBA, error) {
	decompressed := image.NewRGBA(image.Rect(0, 0, width, height))

	blockWidth := (width + 3) / 4
	blockHeight := (height + 3) / 4

loop:
	for blockY := 0; blockY < blockHeight; blockY++ {
		for blockX := 0; blockX < blockWidth; blockX++ {
			if len(compressed) < (blockY*blockWidth+blockX)*16+16 {
				break loop
			}

			blockOffset := (blockY*blockWidth + blockX) * 16 // Each DXT3 block is 16 bytes
			decodeBlockDXT3(compressed[blockOffset:blockOffset+16], decompressed, blockX*4, blockY*4, width)
		}
	}

	return decompressed, nil
}

func decodeBlockDXT3(block []byte, img *image.RGBA, x, y, width int) {
	alphaData := binary.LittleEndian.Uint64(block[0:8])
	c0 := binary.LittleEndian.Uint16(block[8:10])
	c1 := binary.LittleEndian.Uint16(block[10:12])
	colorData := binary.LittleEndian.Uint32(block[12:16])

	colors := make([]color.RGBA, 4)
	colors[0] = rgb565ToRGBAColor(c0)
	colors[1] = rgb565ToRGBAColor(c1)
	if c0 > c1 {
		colors[2] = interpolateColors(colors[0], colors[1], 2, 1)
		colors[3] = interpolateColors(colors[0], colors[1], 1, 2)
	} else {
		colors[2] = interpolateColors(colors[0], colors[1], 1, 1)
		colors[3] = color.RGBA{0, 0, 0, 0}
	}

	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			px := x + i
			py := y + j
			if px >= width {
				continue
			}

			alpha := uint8((alphaData>>uint(j*16+i*4))&0xF) * 17
			colorIndex := (colorData >> uint((j*4+i)*2)) & 0x3
			color := colors[colorIndex]
			color.A = alpha

			img.Set(px, py, color)
		}
	}
}

// DXT4, DXT5 ---------------------------------------------------------------

// decodeDXT5 decodes a DXT5 compressed byte slice into an RGBA image.
func decodeDXT5(compressed []byte, width, height int) (*image.RGBA, error) {
	decompressed := image.NewRGBA(image.Rect(0, 0, width, height))

	blockWidth := (width + 3) / 4
	blockHeight := (height + 3) / 4

loop:
	for blockY := 0; blockY < blockHeight; blockY++ {
		for blockX := 0; blockX < blockWidth; blockX++ {
			if len(compressed) < (blockY*blockWidth+blockX)*16+16 {
				break loop
			}

			blockOffset := (blockY*blockWidth + blockX) * 16 // Each DXT5 block is 16 bytes
			decodeBlockDXT5(compressed[blockOffset:blockOffset+16], decompressed, blockX*4, blockY*4, width)
		}
	}

	return decompressed, nil
}

func decodeBlockDXT5(block []byte, img *image.RGBA, x, y, width int) {
	alpha0 := block[0]
	alpha1 := block[1]
	alphaData := binary.LittleEndian.Uint64(block[0:8]) >> 16
	c0 := binary.LittleEndian.Uint16(block[8:10])
	c1 := binary.LittleEndian.Uint16(block[10:12])
	colorData := binary.LittleEndian.Uint32(block[12:16])

	colors := make([]color.RGBA, 4)
	colors[0] = rgb565ToRGBAColor(c0)
	colors[1] = rgb565ToRGBAColor(c1)
	if c0 > c1 {
		colors[2] = interpolateColors(colors[0], colors[1], 2, 1)
		colors[3] = interpolateColors(colors[0], colors[1], 1, 2)
	} else {
		colors[2] = interpolateColors(colors[0], colors[1], 1, 1)
		colors[3] = color.RGBA{0, 0, 0, 0}
	}

	for j := 0; j < 4; j++ {
		for i := 0; i < 4; i++ {
			px := x + i
			py := y + j
			if px >= width {
				continue
			}

			alphaCode := (alphaData >> uint(j*12+i*3)) & 0x7
			var alpha uint8
			switch alphaCode {
			case 0:
				alpha = alpha0
			case 1:
				alpha = alpha1
			case 2:
				if alpha0 > alpha1 {
					alpha = (6*alpha0 + 1*alpha1) / 7
				} else {
					alpha = (4*alpha0 + 1*alpha1) / 5
				}
			case 3:
				if alpha0 > alpha1 {
					alpha = (5*alpha0 + 2*alpha1) / 7
				} else {
					alpha = (3*alpha0 + 2*alpha1) / 5
				}
			case 4:
				if alpha0 > alpha1 {
					alpha = (4*alpha0 + 3*alpha1) / 7
				} else {
					alpha = (2*alpha0 + 3*alpha1) / 5
				}
			case 5:
				if alpha0 > alpha1 {
					alpha = (3*alpha0 + 4*alpha1) / 7
				} else {
					alpha = (1*alpha0 + 4*alpha1) / 5
				}
			case 6:
				if alpha0 > alpha1 {
					alpha = (2*alpha0 + 5*alpha1) / 7
				} else {
					alpha = alpha1
				}
			case 7:
				if alpha0 > alpha1 {
					alpha = (1*alpha0 + 6*alpha1) / 7
				} else {
					alpha = alpha0
				}
			}

			colorIndex := (colorData >> uint((j*4+i)*2)) & 0x3
			color := colors[colorIndex]
			color.A = alpha

			img.Set(px, py, color)
		}
	}
}
