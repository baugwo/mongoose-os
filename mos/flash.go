// +build !noflash

package main

import (
	"runtime"
	"strings"
	"time"

	"context"

	"cesanta.com/common/go/ourutil"
	"cesanta.com/mos/dev"
	"cesanta.com/mos/flash/cc3200"
	"cesanta.com/mos/flash/cc3220"
	"cesanta.com/mos/flash/common"
	"cesanta.com/mos/flash/esp"
	espFlasher "cesanta.com/mos/flash/esp/flasher"
	"cesanta.com/mos/flash/stm32"
	"github.com/cesanta/errors"
	flag "github.com/spf13/pflag"
)

var (
	cc3200FlashOpts cc3200.FlashOpts
	cc3220FlashOpts cc3220.FlashOpts
	espFlashOpts    esp.FlashOpts
	stm32FlashOpts  stm32.FlashOpts
)

// register advanced flash specific commands
func init() {
	// CC3200
	flag.IntVar(&cc3200FlashOpts.FormatSLFSSize, "cc3200-format-slfs-size", 1048576,
		"Format SLFS for this flash size (bytes)")
	// CC3220
	flag.StringVar(&cc3220FlashOpts.BPIBinary, "cc3220-bpi-binary", "",
		"Path to BuildProgrammingImage binary. If not set will try looking in the default TI dir.")

	// ESP8266, ESP32
	flag.UintVar(&espFlashOpts.ROMBaudRate, "esp-rom-baud-rate", 115200,
		"Data port speed when talking to ROM loader")
	flag.UintVar(&espFlashOpts.FlasherBaudRate, "esp-baud-rate", 460800,
		"Data port speed during flashing")
	flag.StringVar(&espFlashOpts.DataPort, "esp-data-port", "",
		"If specified, this port will be used to send data during flashing. "+
			"If not set, --port is used.")
	flag.StringVar(&espFlashOpts.FlashParams, "esp-flash-params", "",
		"Flash chip params. Either a comma-separated string of mode,size,freq or a number. "+
			"Mode must be one of: qio, qout, dio, dout. "+
			"Valid values for size are: 2m, 4m, 8m, 16m, 32m, 16m-c1, 32m-c1, 32m-c2. "+
			"If left empty, an attempt will be made to auto-detect. freq is SPI frequency "+
			"and can be one of 20m, 26m, 40m, 80m")
	flag.BoolVar(&espFlashOpts.EraseChip, "esp-erase-chip", false,
		"Erase entire chip before flashing")
	flag.BoolVar(&espFlashOpts.EnableCompression, "esp-enable-compression", true,
		"Compress data while writing to flash. Usually makes flashing faster.")
	flag.BoolVar(&espFlashOpts.MinimizeWrites, "esp-minimize-writes", true,
		"Minimize the number of blocks to write by comparing current contents "+
			"with the images being written")
	flag.BoolVar(&espFlashOpts.BootFirmware, "esp-boot-after-flashing", true,
		"Boot the firmware after flashing")
	flag.StringVar(&espFlashOpts.ESP32EncryptionKeyFile, "esp32-encryption-key-file", "",
		"If specified, this file will be used to encrypt data before flashing. "+
			"Encryption is only applied to parts with encrypt=true.")
	flag.Uint32Var(&espFlashOpts.ESP32FlashCryptConf, "esp32-flash-crypt-conf", 0xf,
		"Value of the FLASH_CRYPT_CONF eFuse setting, affecting how key is tweaked.")

	// STM32
	if runtime.GOOS == "windows" {
		// STM32 Windows driver _sometimes_ removes .bin file quite unhurriedly,
		// and flasher prints an error even if flashing itself was successfull
		// For the rest of OSes use smaller timeout though
		flag.DurationVar(&stm32FlashOpts.Timeout, "flash-timeout", 60*time.Second, "Maximum flashing time")
	} else {
		flag.DurationVar(&stm32FlashOpts.Timeout, "flash-timeout", 30*time.Second, "Maximum flashing time")
	}
	// add these flags to the hiddenFlags list so that they can be hidden and shown again with --helpfull
	flag.VisitAll(func(f *flag.Flag) {
		if strings.HasPrefix(f.Name, "cc3200-") || strings.HasPrefix(f.Name, "esp-") || strings.HasPrefix(f.Name, "esp32-") {
			hiddenFlags = append(hiddenFlags, f.Name)
		}
	})
}

func flash(ctx context.Context, devConn *dev.DevConn) error {
	fwname := *firmware
	args := flag.Args()
	if len(args) == 2 {
		fwname = args[1]
	}

	fw, err := common.NewZipFirmwareBundle(fwname)
	if err != nil {
		return errors.Annotatef(err, "failed to load %s", fwname)
	}
	if !*keepTempFiles {
		defer fw.Cleanup()
	}

	ourutil.Reportf("Loaded %s/%s version %s (%s)", fw.Name, fw.Platform, fw.Version, fw.BuildID)

	// if given devConn is not nill, we should disconnect it while flashing is
	// in progress
	if devConn != nil {
		devConn.Disconnect(ctx)
		defer devConn.Connect(ctx, devConn.Reconnect)
	}

	port, err := getPort()
	if err != nil {
		return errors.Trace(err)
	}

	espFlashOpts.InvertedControlLines = *invertedControlLines

	switch strings.ToLower(fw.Platform) {
	case "cc3200":
		cc3200FlashOpts.Port = port
		err = cc3200.Flash(fw, &cc3200FlashOpts)
	case "cc3220":
		cc3220FlashOpts.Port = port
		err = cc3220.Flash(fw, &cc3220FlashOpts)
	case "esp32":
		espFlashOpts.ControlPort = port
		err = espFlasher.Flash(esp.ChipESP32, fw, &espFlashOpts)
	case "esp8266":
		espFlashOpts.ControlPort = port
		err = espFlasher.Flash(esp.ChipESP8266, fw, &espFlashOpts)
	case "stm32":
		stm32FlashOpts.ShareName = port
		err = stm32.Flash(fw, &stm32FlashOpts)
	default:
		err = errors.Errorf("%s: unsupported platform '%s'", *firmware, fw.Platform)
	}

	if err == nil {
		ourutil.Reportf("All done!")
	}

	return errors.Trace(err)
}
