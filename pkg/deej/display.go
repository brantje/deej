package deej

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fcjr/geticon"
	"github.com/golang/freetype"
	"github.com/nfnt/resize"
	"github.com/omriharel/deej/pkg/deej/util"
	"github.com/shirou/gopsutil/v3/process"
	"go.uber.org/zap"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/sys/windows"
)

type (
	ImageType string
	HANDLE    uintptr
	HWND      HANDLE
)

const (
	IconSmall ImageType = "ICON_SMALL"
	IconBig   ImageType = "ICON_BIG"
	JPEG      ImageType = "jpeg"
	PNG       ImageType = "png"
	GIF       ImageType = "gif"
	BMP       ImageType = "bmp"
	UNKNOWN   ImageType = "unknown"
)

var (
	mod                          = windows.NewLazyDLL("user32.dll")
	procGetWindowThreadProcessId = mod.NewProc("GetWindowThreadProcessId")
	modKernel32                  = windows.NewLazyDLL("kernel32.dll")
	modPsapi                     = windows.NewLazyDLL("psapi.dll")

	procOpenProcess       = modKernel32.NewProc("OpenProcess")
	procGetModuleBaseName = modPsapi.NewProc("GetModuleBaseNameW")
	lastActiceProcess     = ""
)

type DisplayConfig struct {
	Enabled         bool
	DitherThreshold int
	DisplayMapping  []DisplayMap
}

type DeejDisplay struct {
	deej   *Deej
	logger *zap.SugaredLogger
}

type DisplayMap struct {
	display_idx int
	target      string
	currentApp  bool
}

func newDisplayConfig() *DisplayConfig {
	return &DisplayConfig{
		Enabled:         false,
		DitherThreshold: 127,
		DisplayMapping:  []DisplayMap{},
	}
}

func NewDeejDisplay(deej *Deej, logger *zap.SugaredLogger) (*DeejDisplay, error) {
	logger = logger.Named("Display")
	display := &DeejDisplay{
		deej:   deej,
		logger: logger,
	}

	logger.Debug("Created display instance")

	return display, nil
}

func createDisplayMapFromConfig(userMapping map[string][]string, userSliderMap *sliderMap) []DisplayMap {
	mapping := []DisplayMap{}
	// Auto grabs the icon from the exe defined in the slider map

	for display_idx, target := range userMapping {
		firstTarget := target[len(target)-1]
		idx, _ := strconv.Atoi(display_idx)

		if strings.Contains(firstTarget, ".exe") {
			mapping = append(mapping, DisplayMap{
				display_idx: idx,
				target:      firstTarget,
			})
		}
		if strings.Contains(firstTarget, ".png") {
			mapping = append(mapping, DisplayMap{
				display_idx: idx,
				target:      firstTarget,
			})
		}
		if firstTarget == "auto" {
			mappedTo, _ := userSliderMap.get(idx)
			firstExe := mappedTo[len(mappedTo)-1]

			isCurrent := firstExe == "deej.current" || firstExe == "deej.unmapped"

			mapping = append(mapping, DisplayMap{
				display_idx: idx,
				target:      firstExe,
				currentApp:  isCurrent,
			})
		}
	}
	fmt.Printf("auto target %v\n", mapping)

	return mapping
}

func (deejDisplay *DeejDisplay) initDisplays() {
	deejDisplay.renderDisplays()

	// Create a channel to receive OS signals
	signalChan := make(chan os.Signal, 1)

	// Notify signalChan when SIGINT or SIGTERM is received
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	configReloadedChannel := deejDisplay.deej.config.SubscribeToChanges()

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				activeWindows, _ := util.GetCurrentWindowProcessNames()
				activeWindow := activeWindows[len(activeWindows)-1]
				if activeWindow != lastActiceProcess {
					lastActiceProcess = activeWindow
					for _, displayConfig := range deejDisplay.deej.config.DisplayConfig.DisplayMapping {
						if displayConfig.currentApp {
							deejDisplay.sendProcessIconToDisplayByProcessName(activeWindow, displayConfig.display_idx)
						}
					}
				}
			case <-configReloadedChannel:
				deejDisplay.renderDisplays()

			case <-signalChan:
				// Exit the goroutine when receiving a termination signal
				return
			}
		}
	}()

}

func (deejDisplay *DeejDisplay) renderDisplays() {
	for _, displayMap := range deejDisplay.deej.config.DisplayConfig.DisplayMapping {
		// .exe is found grab icon from running process
		if strings.Contains(displayMap.target, ".exe") {
			deejDisplay.sendProcessIconToDisplayByProcessName(displayMap.target, displayMap.display_idx)
		}
		if strings.Contains(displayMap.target, ".png") {
			deejDisplay.sendPNGToDisplay(displayMap.target, displayMap.display_idx)
		}
	}
}

func (deejDisplay *DeejDisplay) sendPNGToDisplay(imagePath string, display_idx int) {
	byteSlice := deejDisplay.readFile(imagePath)

	img, err := png.Decode(bytes.NewReader(byteSlice))
	if err != nil {
		log.Fatalf("Error loading PNG icon: %v", err)
	}

	byteSlice = deejDisplay.convertForDisplay(img, false, false)
	deejDisplay.sendData(display_idx, byteSlice)
}

func (deejDisplay *DeejDisplay) sendProcessIconToDisplayByProcessName(processName string, display_idx int) {
	deejDisplay.logger.Debug(fmt.Sprintf("Fetching process icon for %s", processName))
	pid, err := deejDisplay.getPIDByExeName(processName) // I assume this function is implemented elsewhere in your package
	deejDisplay.logger.Info("Got PID: ", pid)
	if err != nil || pid == 0 {
		return
	}

	deejDisplay.sendProcessIconToDisplayByPid(pid, display_idx)
}

func (deejDisplay *DeejDisplay) sendProcessIconToDisplayByPid(pid uint32, display_idx int) {
	deejDisplay.logger.Debug("Fetching icon")
	icon, err := geticon.FromPid(pid)
	if err != nil {
		deejDisplay.logger.Debug("Error fetching icon: ", err)
		return
	}
	byteData := deejDisplay.convertForDisplay(icon, true, true)
	deejDisplay.sendData(display_idx, byteData)
}

func (deejDisplay *DeejDisplay) sendData(display_idx int, data []byte) {
	if deejDisplay.deej.serial.connected {
		deejDisplay.logger.Debug(fmt.Sprintf("Writing to display %d", display_idx))
		sendData := append([]byte(fmt.Sprintf("<<START>>%d|", display_idx)), data...)
		sendData = append(sendData, []byte("<<END>>.....")...)

		_, err := deejDisplay.deej.serial.conn.Write(sendData)
		deejDisplay.checkError("Writing data to port", err)
	} else {
		deejDisplay.logger.Warn("Not connected, skip sending data")
	}
}

func (deejDisplay *DeejDisplay) readFile(filePath string) []byte {
	data, err := os.ReadFile(filePath)
	deejDisplay.checkError(fmt.Sprintf("Reading file: %s", filePath), err)
	return data
}

func (deejDisplay *DeejDisplay) checkError(msg string, err error) {
	if err != nil {
		deejDisplay.logger.Error("%s: %v", msg, err)
	}
}

func (deejDisplay *DeejDisplay) getPIDByExeName(exeName string) (uint32, error) {
	procs, err := process.Processes()
	if err != nil {
		return 0, err
	}
	deejDisplay.logger.Debug("Got process list")
	for _, p := range procs {
		name, err := p.Name()
		if err == nil && strings.ToLower(name) == strings.ToLower(exeName) {
			return uint32(p.Pid), nil
		}
	}
	return 0, fmt.Errorf("no process found with exe name: %s", exeName)
}

func (deejDisplay *DeejDisplay) convertForDisplay(src image.Image, doResize bool, dithering bool) []byte {
	// imageType, err := DetectImageTypeFromImage(src)
	// if err != nil {
	// 	fmt.Println(err)
	// 	return []byte{}
	// }
	// fmt.Printf("Detected icon image type: %s\n", imageType)
	// Resize to 50x50
	deejDisplay.logger.Debug("Converting icon to for display")
	var resizedImg image.Image
	if doResize {
		deejDisplay.logger.Debug("Resize to 60x60")
		resizedImg = resize.Resize(60, 60, src, resize.Lanczos3)
	} else {
		resizedImg = src
	}
	// Create a blank canvas of size 128x64
	deejDisplay.logger.Debug("Create new 128x64 image")
	canvas := image.NewRGBA(image.Rect(0, 0, 128, 64))

	// Compute the top-left corner coordinates for centering the image
	startX := (canvas.Bounds().Dx() - resizedImg.Bounds().Dx()) / 2
	startY := (canvas.Bounds().Dy() - resizedImg.Bounds().Dy()) / 2

	deejDisplay.logger.Debug("Draw image on center")
	// Draw the resized image onto the canvas
	for y := 0; y < resizedImg.Bounds().Dy(); y++ {
		for x := 0; x < resizedImg.Bounds().Dx(); x++ {
			canvas.Set(startX+x, startY+y, resizedImg.At(x, y))
		}
	}

	// canvas = deejDisplay.drawNumberOnImage(canvas, 10)

	// Convert the canvas to black and white
	if dithering {
		deejDisplay.logger.Debug("Convert to black and white")
		canvas = deejDisplay.floydSteinbergDithering(canvas)
	}
	deejDisplay.logger.Debug("Coverting to 1 bit image")
	buf := deejDisplay.encode1Bit(canvas)

	return buf
}

func (deejDisplay *DeejDisplay) drawNumberOnImage(img image.Image, number int) *image.RGBA {
	deejDisplay.logger.Debug("Drawing number on image")

	// Convert the number to string
	text := fmt.Sprintf("%d", number)

	// Create a new image based on the original for drawing
	dst := image.NewRGBA(img.Bounds())

	// Copy the original image onto dst
	draw.Draw(dst, dst.Bounds(), img, image.Point{}, draw.Over)

	// Initialize the freetype context
	c := freetype.NewContext()

	// Set the font
	f, err := freetype.ParseFont(goregular.TTF)
	if err != nil {
		log.Fatalf("Could not parse font: %v", err)
	}
	c.SetFont(f)

	fontSize := float64(16)
	// Set various properties
	c.SetFontSize(fontSize) // Adjust as necessary
	c.SetClip(dst.Bounds())
	c.SetDst(dst)
	c.SetSrc(image.NewUniform(color.RGBA{R: 255, G: 255, B: 255, A: 255})) // Red color

	// Calculate the width of the text to align it to the top right corner
	// pt := freetype.Pt(img.Bounds().Max.X-int(c.PointToFixed(24)>>6)*len(text), int(c.PointToFixed(24)>>6)-10)
	pt := freetype.Pt(img.Bounds().Max.X-int(c.PointToFixed(fontSize)>>6)*len(text)+10, int(c.PointToFixed(fontSize)>>6))
	// Draw the text
	_, err = c.DrawString(text, pt)
	if err != nil {
		log.Fatalf("Could not draw text: %v", err)
	}

	return dst
}

func otsuThreshold(img *image.RGBA) uint8 {
	// Step 1: Compute histogram and total pixel count
	var histogram [256]int
	totalPixels := img.Bounds().Dx() * img.Bounds().Dy()

	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			grayValue := color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
			histogram[grayValue]++
		}
	}

	// Calculate cumulative sum and cumulative sum of pixel values
	sum, sumB, wB, wF, threshold := 0, 0, 0, 0, 0
	var mB, mF, between, maxBetween float64
	for i := 0; i < 256; i++ {
		sum += i * histogram[i]
	}

	// Step 2 & 3: Compute Otsu's threshold using the computed values
	for t := 0; t < 256; t++ {
		wB += histogram[t]
		if wB == 0 {
			continue
		}
		wF = totalPixels - wB
		if wF == 0 {
			break
		}

		sumB += t * histogram[t]
		mB = float64(sumB) / float64(wB)
		mF = (float64(sum) - float64(sumB)) / float64(wF)

		between = float64(wB) * float64(wF) * (mB - mF) * (mB - mF)
		if between > maxBetween {
			maxBetween = between
			threshold = t
		}
	}

	return uint8(threshold)
}

func (deejDisplay *DeejDisplay) ditherPixel(x, y int, img *image.RGBA, errMatrix [][]float32, threshold uint8) color.RGBA {
	oldPixelColor := color.GrayModel.Convert(img.At(x, y))
	oldPixel := oldPixelColor.(color.Gray).Y

	newPixel := color.RGBA{}
	if oldPixel > threshold { // Default 127
		newPixel = color.RGBA{255, 255, 255, 255}
	} else {
		newPixel = color.RGBA{0, 0, 0, 255}
	}

	quantError := float32(oldPixel) - float32(newPixel.R) // Using R because it's identical in grayscale for RGB

	if x+1 < img.Bounds().Max.X {
		img.Pix[y*img.Stride+4*(x+1)] = uint8(float32(img.Pix[y*img.Stride+4*(x+1)]) + quantError*errMatrix[0][1])
	}
	if x-1 >= 0 && y+1 < img.Bounds().Max.Y {
		img.Pix[(y+1)*img.Stride+4*(x-1)] = uint8(float32(img.Pix[(y+1)*img.Stride+4*(x-1)]) + quantError*errMatrix[1][0])
	}
	if y+1 < img.Bounds().Max.Y {
		img.Pix[(y+1)*img.Stride+4*x] = uint8(float32(img.Pix[(y+1)*img.Stride+4*x]) + quantError*errMatrix[1][1])
	}
	if x+1 < img.Bounds().Max.X && y+1 < img.Bounds().Max.Y {
		img.Pix[(y+1)*img.Stride+4*(x+1)] = uint8(float32(img.Pix[(y+1)*img.Stride+4*(x+1)]) + quantError*errMatrix[1][2])
	}
	return newPixel
}

func (deejDisplay *DeejDisplay) floydSteinbergDithering(src *image.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dest := image.NewRGBA(bounds)
	threshold := otsuThreshold(src)
	threshold = threshold + 25
	errMatrix := [][]float32{
		{0, 7.0 / 16.0},
		{3.0 / 16.0, 5.0 / 16.0, 1.0 / 16.0},
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dest.Set(x, y, deejDisplay.ditherPixel(x, y, src, errMatrix, threshold))
		}
	}
	return dest
}

func (deejDisplay *DeejDisplay) encode1Bit(img image.Image) []byte {
	sz := img.Bounds()
	buff := new(bytes.Buffer)

	var currentByte uint8
	var bitPosition uint8 = 7

	for y := sz.Min.Y; y < sz.Max.Y; y++ {
		for x := sz.Min.X; x < sz.Max.X; x++ {
			c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if c.Y > 127 {
				currentByte |= (1 << bitPosition)
			}

			if bitPosition == 0 { // If we've set all bits for the current byte
				buff.WriteByte(currentByte)
				currentByte = 0
				bitPosition = 7
			} else {
				bitPosition--
			}
		}
	}

	// Write any remaining bits
	if bitPosition != 7 {
		buff.WriteByte(currentByte)
	}

	return buff.Bytes()
}
