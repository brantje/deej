package deej

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math/rand"
	"os"

	"github.com/fcjr/geticon"
	"github.com/jacobsa/go-serial/serial"
	"github.com/nfnt/resize"
	"github.com/shirou/gopsutil/v3/process"
)

type ImageType string

const (
	IconSmall ImageType = "ICON_SMALL"
	IconBig   ImageType = "ICON_BIG"
	JPEG      ImageType = "jpeg"
	PNG       ImageType = "png"
	GIF       ImageType = "gif"
	BMP       ImageType = "bmp"
	UNKNOWN   ImageType = "unknown"
)

type DisplayConfig struct {
	UseDisplays     bool           `mapstructure:"use_displays"`
	DitherThreshold int            `mapstructure:"dither_threshold"`
	DisplayMapping  map[int]string `mapstructure:"display_mapping"`
}

func newDisplayConfig() *DisplayConfig {
	return &DisplayConfig{
		UseDisplays:     false,
		DitherThreshold: 127,
		DisplayMapping:  make(map[int]string),
	}
}

func main() {
	options := getSerialOptions()
	port := openSerialPort(options)
	defer port.Close()

	sendPNGToDisplay(port, "test.png")
	sendProcessIconToDisplay(port)

	// For speaker icons on the remaining screens, consider storing the icon data in a separate file or in a different format for clarity.
}

func getSerialOptions() serial.OpenOptions {
	return serial.OpenOptions{
		PortName:              "COM15",
		BaudRate:              115200,
		DataBits:              8,
		StopBits:              1,
		MinimumReadSize:       4,
		InterCharacterTimeout: 100,
	}
}

func openSerialPort(options serial.OpenOptions) io.ReadWriteCloser {
	port, err := serial.Open(options)
	if err != nil {
		log.Fatalf("serial.Open: %v", err)
	}
	return port
}

func sendPNGToDisplay(port io.Writer, imagePath string) {
	byteSlice := readFile(imagePath)

	img, err := png.Decode(bytes.NewReader(byteSlice))
	if err != nil {
		log.Fatalf("Load BMP icon: %v", err)
	}

	byteSlice = convertForDisplay(img, false)
	sendData(port, byteSlice, 1)
}

func sendProcessIconToDisplay(port io.Writer) {
	procs, err := process.Processes()
	checkError("Fetching processes", err)

	n := rand.Int() % len(procs)
	randomProc := procs[n]
	processName, _ := randomProc.Name()

	pid, err := getPIDByExeName(processName) // I assume this function is implemented elsewhere in your package
	checkError(fmt.Sprintf("Fetching PID for process: %s", processName), err)

	icon, err := geticon.FromPid(pid)
	checkError("Fetching icon", err)

	byteData := convertForDisplay(icon, true)
	sendData(port, byteData, 3)
}

func sendData(port io.Writer, data []byte, id int) {
	sendData := append([]byte(fmt.Sprintf("<<START>>%d|", id)), data...)
	sendData = append(sendData, []byte("<<END>>.....")...)

	_, err := port.Write(sendData)
	checkError("Writing data to port", err)
}

func readFile(filePath string) []byte {
	data, err := os.ReadFile(filePath)
	checkError(fmt.Sprintf("Reading file: %s", filePath), err)
	return data
}

func checkError(msg string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func getPIDByExeName(exeName string) (uint32, error) {
	procs, err := process.Processes()
	if err != nil {
		return 0, err
	}

	for _, p := range procs {
		name, err := p.Name()
		if err == nil && name == exeName {
			return uint32(p.Pid), nil
		}
	}

	return 0, fmt.Errorf("no process found with exe name: %s", exeName)
}

func convertForDisplay(src image.Image, doResize bool) []byte {
	// imageType, err := DetectImageTypeFromImage(src)
	// if err != nil {
	// 	fmt.Println(err)
	// 	return []byte{}
	// }
	// fmt.Printf("Detected icon image type: %s\n", imageType)
	// Resize to 50x50
	fmt.Println("Converting icon to for display")
	var resizedImg image.Image
	if doResize {
		fmt.Println("Resize to 60x60")
		resizedImg = resize.Resize(60, 60, src, resize.Lanczos3)
	} else {
		resizedImg = src
	}
	// Create a blank canvas of size 128x64
	fmt.Println("Create new 128x64 image")
	canvas := image.NewRGBA(image.Rect(0, 0, 128, 64))

	// Compute the top-left corner coordinates for centering the image
	startX := (canvas.Bounds().Dx() - resizedImg.Bounds().Dx()) / 2
	startY := (canvas.Bounds().Dy() - resizedImg.Bounds().Dy()) / 2

	fmt.Println("Draw image on center")
	// Draw the resized image onto the canvas
	for y := 0; y < resizedImg.Bounds().Dy(); y++ {
		for x := 0; x < resizedImg.Bounds().Dx(); x++ {
			canvas.Set(startX+x, startY+y, resizedImg.At(x, y))
		}
	}

	// Convert the canvas to black and white
	fmt.Println("Convert to black and white")
	bwImg := floydSteinbergDithering(canvas)

	fmt.Println("Coverting to 1 bit image")
	buf := encode1Bit(bwImg)

	return buf
}

func ditherPixel(x, y int, img *image.RGBA, errMatrix [][]float32) color.RGBA {
	oldPixelColor := color.GrayModel.Convert(img.At(x, y))
	oldPixel := oldPixelColor.(color.Gray).Y

	newPixel := color.RGBA{}
	if oldPixel > 101 { // Default 127
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

func floydSteinbergDithering(src *image.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dest := image.NewRGBA(bounds)
	errMatrix := [][]float32{
		{0, 7.0 / 16.0},
		{3.0 / 16.0, 5.0 / 16.0, 1.0 / 16.0},
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dest.Set(x, y, ditherPixel(x, y, src, errMatrix))
		}
	}
	return dest
}

func encode1Bit(img image.Image) []byte {
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
