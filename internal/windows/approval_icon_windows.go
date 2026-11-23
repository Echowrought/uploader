package windows

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"unsafe"
)

func createApprovalImageIcon(img image.Image) (uintptr, error) {
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		return 0, err
	}
	return createApprovalResourceIcon(data.Bytes())
}

func createApprovalResourceIcon(resource []byte) (uintptr, error) {
	const iconResourceVersion = 0x00030000
	icon, _, err := approvalUser32.NewProc("CreateIconFromResourceEx").Call(
		uintptr(unsafe.Pointer(&resource[0])), uintptr(len(resource)), 1, iconResourceVersion, 32, 32, 0)
	if icon == 0 {
		return 0, fmt.Errorf("create authentication dialog icon: %w", err)
	}
	return icon, nil
}

func createApprovalTitleIcon() (uintptr, error) {
	count := int(binary.LittleEndian.Uint16(trayIcon[4:6]))
	for i := range count {
		entry := trayIcon[6+16*i:]
		if entry[0] == 32 && entry[1] == 32 {
			size := binary.LittleEndian.Uint32(entry[8:])
			start := binary.LittleEndian.Uint32(entry[12:])
			return createApprovalResourceIcon(trayIcon[start : start+size])
		}
	}
	return 0, fmt.Errorf("tray artwork has no 32-pixel icon")
}

const spinnerFrameCount = 8

// Font Awesome Free 6.7.2 spinner, © 2024 Fonticons, Inc., CC BY 4.0.
// Rasterized from the official 512-unit circles, recolored and rotated.
// https://github.com/FortAwesome/Font-Awesome/blob/6.7.2/svgs/solid/spinner.svg
func approvalSpinnerImage(frame int) *image.RGBA {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	centers := [...][2]float64{{256, 48}, {256, 464}, {48, 256}, {464, 256}, {109, 403}, {109, 109}, {403, 403}}
	sin, cos := math.Sincos(float64(frame) * (2 * math.Pi / spinnerFrameCount))
	for i, point := range centers {
		x, y := point[0]-256, point[1]-256
		centers[i] = [2]float64{(x*cos - y*sin + 256) / 8, (x*sin + y*cos + 256) / 8}
	}
	blue := color.RGBA{R: 61, G: 134, B: 200, A: 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			for _, center := range centers {
				if math.Hypot(float64(x)+.5-center[0], float64(y)+.5-center[1]) <= 6 {
					img.SetRGBA(x, y, blue)
					break
				}
			}
		}
	}
	return img
}

func approvalCheckImage() *image.RGBA {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	green := color.RGBA{R: 22, G: 138, B: 65, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+.5, float64(y)+.5
			if math.Hypot(px-32, py-32) > 30 {
				continue
			}
			if segmentDistance(px, py, 18, 32, 28, 42) < 3.5 || segmentDistance(px, py, 28, 42, 46, 22) < 3.5 {
				img.SetRGBA(x, y, white)
			} else {
				img.SetRGBA(x, y, green)
			}
		}
	}
	return img
}

func segmentDistance(x, y, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := max(0, min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-ax-t*dx, y-ay-t*dy)
}
