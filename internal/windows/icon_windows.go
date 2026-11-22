package windows

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"
)

//go:embed icon.ico
var trayIcon []byte

var errorTrayIcon = sync.OnceValue(func() []byte { return badgeIcon(trayIcon) })

// The committed ICO contains a PNG for each supported DPI. Add the same badge
// at each size and retain the original ICO entries and their dimensions.
func badgeIcon(icon []byte) []byte {
	count := int(binary.LittleEndian.Uint16(icon[4:6]))
	headerSize := 6 + 16*count
	result := append([]byte(nil), icon[:headerSize]...)
	for i := 0; i < count; i++ {
		entry := 6 + i*16
		size := int(binary.LittleEndian.Uint32(icon[entry+8:]))
		offset := int(binary.LittleEndian.Uint32(icon[entry+12:]))
		original, err := png.Decode(bytes.NewReader(icon[offset : offset+size]))
		if err != nil {
			return icon
		}
		badged := image.NewRGBA(original.Bounds())
		draw.Draw(badged, badged.Bounds(), original, original.Bounds().Min, draw.Src)
		addErrorBadge(badged)
		var pngData bytes.Buffer
		if err := png.Encode(&pngData, badged); err != nil {
			return icon
		}
		binary.LittleEndian.PutUint32(result[entry+8:], uint32(pngData.Len()))
		binary.LittleEndian.PutUint32(result[entry+12:], uint32(len(result)))
		result = append(result, pngData.Bytes()...)
	}
	return result
}

func addErrorBadge(img *image.RGBA) {
	bounds := img.Bounds()
	scale := float64(bounds.Dx())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx := (float64(x-bounds.Min.X)+.5)/scale - .72
			dy := (float64(y-bounds.Min.Y)+.5)/scale - .72
			radius := math.Hypot(dx, dy)
			switch {
			case radius > .275:
			case radius > .235:
				img.SetRGBA(x, y, color.RGBA{R: 26, G: 10, B: 14, A: 255})
			case math.Max(math.Abs(dx), math.Abs(dy)) < .13 && math.Min(math.Abs(dx-dy), math.Abs(dx+dy)) < .065:
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			default:
				img.SetRGBA(x, y, color.RGBA{R: 220, G: 38, B: 51, A: 255})
			}
		}
	}
}
