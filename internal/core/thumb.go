package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"sync"
)

// Thumbnails are made where the picture is, so that a folder of photos shows
// without every photo crossing the network.

const (
	thumbMaxPixels = 80 << 20 // refuse to decode anything larger
	thumbKept      = 400
)

type thumbCache struct {
	mu    sync.Mutex
	items map[string][]byte
	order []string
	// slots keeps the decoding of large pictures from eating all the memory.
	slots chan struct{}
}

var thumbs = thumbCache{items: map[string][]byte{}, slots: make(chan struct{}, 2)}

func (c *Core) thumbnail(path string, size int) ([]byte, error) {
	if size < 32 || size > 512 {
		size = 192
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, plain(err)
	}
	key := fmt.Sprintf("%s|%d|%d|%d", path, info.ModTime().UnixNano(), info.Size(), size)
	thumbs.mu.Lock()
	data, ok := thumbs.items[key]
	thumbs.mu.Unlock()
	if ok {
		return data, nil
	}

	thumbs.slots <- struct{}{}
	defer func() { <-thumbs.slots }()
	f, err := os.Open(path)
	if err != nil {
		return nil, plain(err)
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil || config.Width*config.Height > thumbMaxPixels {
		return nil, errors.New("no preview for this file")
	}
	f.Seek(0, io.SeekStart)
	orientation := exifOrientation(f)
	f.Seek(0, io.SeekStart)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, errors.New("no preview for this file")
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, shrink(img, size, orientation), &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	thumbs.mu.Lock()
	thumbs.items[key] = out.Bytes()
	thumbs.order = append(thumbs.order, key)
	if len(thumbs.order) > thumbKept {
		delete(thumbs.items, thumbs.order[0])
		thumbs.order = thumbs.order[1:]
	}
	thumbs.mu.Unlock()
	return out.Bytes(), nil
}

// shrink scales img to fit a square of the given size and turns it the way
// the camera said it was held.
func shrink(img image.Image, size, orientation int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := float64(size) / float64(max(w, h))
	if scale > 1 {
		scale = 1
	}
	tw, th := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	swap := orientation >= 5
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	if swap {
		dst = image.NewRGBA(image.Rect(0, 0, th, tw))
	}
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			// Four samples per pixel soften what plain skipping would alias.
			var r, g, bl, a uint32
			for _, o := range [4][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}} {
				sx := b.Min.X + int((float64(x)+o[0])*float64(w)/float64(tw))
				sy := b.Min.Y + int((float64(y)+o[1])*float64(h)/float64(th))
				pr, pg, pb, pa := img.At(min(sx, b.Max.X-1), min(sy, b.Max.Y-1)).RGBA()
				r, g, bl, a = r+pr, g+pg, bl+pb, a+pa
			}
			px := color.RGBA{uint8(r >> 10), uint8(g >> 10), uint8(bl >> 10), uint8(a >> 10)}
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = tw - 1 - x
			case 3:
				dx, dy = tw-1-x, th-1-y
			case 4:
				dy = th - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = th-1-y, x
			case 7:
				dx, dy = th-1-y, tw-1-x
			case 8:
				dx, dy = y, tw-1-x
			}
			dst.SetRGBA(dx, dy, px)
		}
	}
	return dst
}

// exifOrientation reads how a JPEG should be turned: 1 to 8, 1 for upright
// and for anything that does not say.
func exifOrientation(r io.Reader) int {
	head := make([]byte, 128*1024)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	if len(head) < 4 || head[0] != 0xff || head[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(head) && head[pos] == 0xff; {
		marker, length := head[pos+1], int(binary.BigEndian.Uint16(head[pos+2:]))
		seg := head[pos+4 : min(len(head), pos+2+length)]
		pos += 2 + length
		if marker != 0xe1 || len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := seg[6:]
		var order binary.ByteOrder = binary.BigEndian
		if tiff[0] == 'I' {
			order = binary.LittleEndian
		}
		ifd := int(order.Uint32(tiff[4:]))
		if ifd+2 > len(tiff) {
			return 1
		}
		count := int(order.Uint16(tiff[ifd:]))
		for i := 0; i < count; i++ {
			entry := ifd + 2 + i*12
			if entry+12 > len(tiff) {
				return 1
			}
			if order.Uint16(tiff[entry:]) == 0x0112 {
				if v := int(order.Uint16(tiff[entry+8:])); v >= 1 && v <= 8 {
					return v
				}
				return 1
			}
		}
		return 1
	}
	return 1
}
