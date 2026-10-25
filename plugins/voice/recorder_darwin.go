//go:build darwin && cgo

package voice

// #cgo LDFLAGS: -framework AudioToolbox -framework CoreAudio -framework CoreFoundation
// #include <stdlib.h>
// #include "audioqueue_darwin.h"
import "C"

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// maxInputDevices bounds one CoreAudio enumeration.
const maxInputDevices = 64

type darwinAudioReadCloser struct {
	file *os.File
	rec  *C.jt_audio_recorder_t
}

func (r *darwinAudioReadCloser) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

func (r *darwinAudioReadCloser) Close() error {
	if r.rec != nil {
		C.jt_audio_stop(r.rec)
		r.rec = nil
	}
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

func listInputDevices() ([]InputDevice, error) {
	buf := make([]C.jt_audio_device_t, maxInputDevices)
	n := int(C.jt_audio_list_input_devices(&buf[0], C.int(len(buf))))
	if n < 0 {
		return nil, fmt.Errorf("CoreAudio: cannot read device list")
	}
	n = min(n, len(buf))
	devices := make([]InputDevice, 0, n)
	for i := range n {
		devices = append(devices, InputDevice{
			UID:  C.GoString(&buf[i].uid[0]),
			Name: C.GoString(&buf[i].name[0]),
			Kind: deviceKind(buf[i].kind),
		})
	}
	return devices, nil
}

func deviceKind(kind C.int) string {
	switch kind {
	case C.JT_DEVICE_MIC:
		return DeviceMic
	case C.JT_DEVICE_LOOPBACK:
		return DeviceLoopback
	default:
		return DeviceUnknown
	}
}

// matchInputDevice picks the device whose UID equals want exactly, else the
// first whose name equals want ignoring case.
func matchInputDevice(devices []InputDevice, want string) (InputDevice, bool) {
	for _, d := range devices {
		if d.UID != "" && d.UID == want {
			return d, true
		}
	}
	for _, d := range devices {
		if strings.EqualFold(d.Name, want) {
			return d, true
		}
	}
	return InputDevice{}, false
}

// resolveDeviceUID maps the configured device to a CoreAudio UID. Empty or
// "default" means the system default input. An unknown device is an error:
// falling back to the default mic would capture the wrong audio.
func resolveDeviceUID(logger *slog.Logger, device string) (string, error) {
	device = strings.TrimSpace(device)
	if device == "" || strings.EqualFold(device, "default") {
		return "", nil
	}
	devices, err := listInputDevices()
	if err != nil {
		return "", err
	}
	d, ok := matchInputDevice(devices, device)
	if !ok {
		names := DeviceNames(devices)
		logger.Error("audio input device not found", "device", device, "available", names)
		return "", fmt.Errorf("未找到音频输入设备 %q；可用输入设备: %s", device, strings.Join(names, ", "))
	}
	logger.Info("audio input device selected", "name", d.Name, "uid", d.UID, "kind", d.Kind)
	return d.UID, nil
}

func startCaptureWithDevice(logger *slog.Logger, device string) (io.ReadCloser, string, func() error, error) {
	uid, err := resolveDeviceUID(logger, device)
	if err != nil {
		return nil, "", nil, err
	}
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		return nil, "", nil, err
	}
	cWriteFD, err := syscall.Dup(int(writeFile.Fd()))
	if err != nil {
		_ = readFile.Close()
		_ = writeFile.Close()
		return nil, "", nil, err
	}

	var cUID *C.char
	if uid != "" {
		cUID = C.CString(uid)
		defer C.free(unsafe.Pointer(cUID))
	}
	var rec *C.jt_audio_recorder_t
	rc := int(C.jt_audio_start(C.int(cWriteFD), cUID, &rec))
	_ = writeFile.Close()
	if rc != 0 {
		_ = syscall.Close(cWriteFD)
		_ = readFile.Close()
		return nil, "", nil, fmt.Errorf("AudioQueue start failed: %d", rc)
	}

	closer := &darwinAudioReadCloser{file: readFile, rec: rec}
	stop := func() error { return closer.Close() }
	return closer, "coreaudio", stop, nil
}

// ListDevices returns every CoreAudio device with input streams. The system
// default input is not listed: an empty device setting selects it.
func ListDevices() ([]InputDevice, error) { return listInputDevices() }
