#pragma once

typedef struct jt_audio_recorder jt_audio_recorder_t;

#define JT_AUDIO_DEVICE_STR 256

// Device kinds reported in jt_audio_device_t.kind. A loopback device carries
// what the computer plays back into an input; a mic captures outside sound.
#define JT_DEVICE_UNKNOWN 0
#define JT_DEVICE_MIC 1
#define JT_DEVICE_LOOPBACK 2

typedef struct {
	char uid[JT_AUDIO_DEVICE_STR];
	char name[JT_AUDIO_DEVICE_STR];
	int kind;
} jt_audio_device_t;

// device_uid NULL or empty records from the system default input device.
int jt_audio_start(int write_fd, const char *device_uid, jt_audio_recorder_t **out_recorder);
void jt_audio_stop(jt_audio_recorder_t *recorder);

// Fills up to cap input-capable devices into out and returns how many input
// devices exist, or -1 when the device list cannot be read.
int jt_audio_list_input_devices(jt_audio_device_t *out, int cap);
