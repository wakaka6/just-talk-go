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

// inputDevice is one CoreAudio device with input streams.
type inputDevice struct {
	UID  string
	Name string
}

func listInputDevices() ([]inputDevice, error) {
	buf := make([]C.jt_audio_device_t, maxInputDevices)
	n := int(C.jt_audio_list_input_devices(&buf[0], C.int(len(buf))))
	if n < 0 {
		return nil, fmt.Errorf("CoreAudio: cannot read device list")
	}
	n = min(n, len(buf))
	devices := make([]inputDevice, 0, n)
	for i := range n {
		devices = append(devices, inputDevice{
			UID:  C.GoString(&buf[i].uid[0]),
			Name: C.GoString(&buf[i].name[0]),
		})
	}
	return devices, nil
}

// matchInputDevice picks the device whose UID equals want exactly, else the
// first whose name equals want ignoring case.
func matchInputDevice(devices []inputDevice, want string) (inputDevice, bool) {
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
	return inputDevice{}, false
}

func inputDeviceNames(devices []inputDevice) []string {
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, d.Name)
	}
	return names
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
		names := inputDeviceNames(devices)
		logger.Error("audio input device not found", "device", device, "available", names)
		return "", fmt.Errorf("未找到音频输入设备 %q；可用输入设备: %s", device, strings.Join(names, ", "))
	}
	logger.Info("audio input device selected", "name", d.Name, "uid", d.UID)
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

// ListDevices returns "default" followed by the names of all input devices.
func ListDevices() ([]string, error) {
	devices, err := listInputDevices()
	if err != nil {
		return nil, err
	}
	return append([]string{"default"}, inputDeviceNames(devices)...), nil
}
