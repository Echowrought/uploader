package windows

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"testing"
)

func TestTrayErrorIconShowsRedBadgeWithWhiteCrossAtEverySize(t *testing.T) {
	icon := errorTrayIcon()
	count := int(binary.LittleEndian.Uint16(trayIcon[4:6]))
	if count != int(binary.LittleEndian.Uint16(icon[4:6])) || count == 0 {
		t.Fatal("badged icon lost original sizes")
	}
	for i := 0; i < count; i++ {
		entry := 6 + 16*i
		if !bytes.Equal(trayIcon[entry:entry+8], icon[entry:entry+8]) {
			t.Fatalf("dimensions or format changed at entry %d", i)
		}
		size := int(binary.LittleEndian.Uint32(icon[entry+8:]))
		offset := int(binary.LittleEndian.Uint32(icon[entry+12:]))
		img, err := png.Decode(bytes.NewReader(icon[offset : offset+size]))
		if err != nil {
			t.Fatal(err)
		}
		dimension := float64(img.Bounds().Dx())
		red := color.RGBA{R: 220, G: 38, B: 51, A: 255}
		white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
		if color.RGBAModel.Convert(img.At(int(.88*dimension), int(.72*dimension))) != red || color.RGBAModel.Convert(img.At(int(.72*dimension), int(.72*dimension))) != white {
			t.Fatalf("entry %d does not show the intended badge", i)
		}
	}
}
