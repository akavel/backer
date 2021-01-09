package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
)

func thumbnailImage(r io.Reader, maxw, maxh int) ([]byte, error) {
	src, typ, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("parsing image for thumbnail: %w", err)
	}

	// Calculate thumbnail size
	srcb := src.Bounds()
	if srcb.Dx() > maxw || srcb.Dy() > maxh {
		sx := float64(maxw) / float64(srcb.Dx())
		sy := float64(maxh) / float64(srcb.Dy())
		scale := sx
		if sy < sx {
			scale = sy
		}
		maxw = int(float64(srcb.Dx()) * scale)
		maxh = int(float64(srcb.Dy()) * scale)
	} else {
		maxw, maxh = srcb.Dx(), srcb.Dy()
	}

	// Render the thumbnail
	dst := image.NewRGBA(image.Rect(0, 0, maxw, maxh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, srcb, draw.Src, nil)

	// Encode the thumbnail back to original format
	buf := bytes.NewBuffer(nil)
	switch typ {
	case "png":
		err = png.Encode(buf, dst)
		if err != nil {
			return nil, fmt.Errorf("encoding png thumbnail: %w", err)
		}
		return buf.Bytes(), nil
	default:
		err = jpeg.Encode(buf, dst, nil)
		if err != nil {
			return nil, fmt.Errorf("encoding jpeg thumbnail: %w", err)
		}
		return buf.Bytes(), nil
	}
}
