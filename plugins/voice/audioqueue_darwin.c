//go:build darwin && cgo

#include "audioqueue_darwin.h"

#include <AudioToolbox/AudioToolbox.h>
#include <CoreAudio/CoreAudio.h>
#include <CoreAudio/CoreAudioTypes.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define JT_BUFFER_COUNT 3
#define JT_BUFFER_BYTES 4096

struct jt_audio_recorder {
	AudioQueueRef queue;
	AudioQueueBufferRef buffers[JT_BUFFER_COUNT];
	int write_fd;
	pthread_mutex_t mu;
	int running;
};

static void jt_audio_cb(void *user_data, AudioQueueRef queue, AudioQueueBufferRef buffer,
			const AudioTimeStamp *start_time, UInt32 packet_count,
			const AudioStreamPacketDescription *packet_desc) {
	(void)start_time;
	(void)packet_count;
	(void)packet_desc;
	jt_audio_recorder_t *rec = (jt_audio_recorder_t *)user_data;

	pthread_mutex_lock(&rec->mu);
	int running = rec->running;
	int fd = rec->write_fd;
	pthread_mutex_unlock(&rec->mu);

	if (running && fd >= 0 && buffer->mAudioDataByteSize > 0) {
		const char *p = (const char *)buffer->mAudioData;
		UInt32 left = buffer->mAudioDataByteSize;
		while (left > 0) {
			ssize_t n = write(fd, p, left);
			if (n <= 0) break;
			p += n;
			left -= (UInt32)n;
		}
	}

	if (running) {
		AudioQueueEnqueueBuffer(queue, buffer, 0, NULL);
	}
}

static int jt_copy_cstring_prop(AudioObjectID dev, AudioObjectPropertySelector sel, char *out, size_t cap) {
	AudioObjectPropertyAddress addr = {sel, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	CFStringRef value = NULL;
	UInt32 size = sizeof(value);
	out[0] = '\0';
	if (AudioObjectGetPropertyData(dev, &addr, 0, NULL, &size, &value) != noErr || value == NULL) return 0;
	Boolean ok = CFStringGetCString(value, out, (CFIndex)cap, kCFStringEncodingUTF8);
	CFRelease(value);
	return ok ? 1 : 0;
}

static int jt_has_input_streams(AudioObjectID dev) {
	AudioObjectPropertyAddress addr = {kAudioDevicePropertyStreams, kAudioObjectPropertyScopeInput,
					   kAudioObjectPropertyElementMain};
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(dev, &addr, 0, NULL, &size) != noErr) return 0;
	return size > 0;
}

int jt_audio_list_input_devices(jt_audio_device_t *out, int cap) {
	AudioObjectPropertyAddress addr = {kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal,
					   kAudioObjectPropertyElementMain};
	UInt32 size = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &addr, 0, NULL, &size) != noErr) return -1;
	UInt32 count = size / sizeof(AudioObjectID);
	if (count == 0) return 0;
	AudioObjectID *ids = (AudioObjectID *)calloc(count, sizeof(AudioObjectID));
	if (ids == NULL) return -1;
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, ids) != noErr) {
		free(ids);
		return -1;
	}
	count = size / sizeof(AudioObjectID);

	int found = 0;
	for (UInt32 i = 0; i < count; i++) {
		if (!jt_has_input_streams(ids[i])) continue;
		if (found < cap) {
			jt_copy_cstring_prop(ids[i], kAudioDevicePropertyDeviceUID, out[found].uid, sizeof(out[found].uid));
			jt_copy_cstring_prop(ids[i], kAudioObjectPropertyName, out[found].name, sizeof(out[found].name));
		}
		found++;
	}
	free(ids);
	return found;
}

int jt_audio_start(int write_fd, const char *device_uid, jt_audio_recorder_t **out_recorder) {
	if (out_recorder == NULL) return -1;
	*out_recorder = NULL;

	jt_audio_recorder_t *rec = (jt_audio_recorder_t *)calloc(1, sizeof(jt_audio_recorder_t));
	if (rec == NULL) return -1;
	rec->write_fd = write_fd;
	rec->running = 1;
	pthread_mutex_init(&rec->mu, NULL);

	AudioStreamBasicDescription fmt;
	memset(&fmt, 0, sizeof(fmt));
	fmt.mSampleRate = 16000.0;
	fmt.mFormatID = kAudioFormatLinearPCM;
	fmt.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked;
	fmt.mBitsPerChannel = 16;
	fmt.mChannelsPerFrame = 1;
	fmt.mFramesPerPacket = 1;
	fmt.mBytesPerFrame = 2;
	fmt.mBytesPerPacket = 2;

	OSStatus err = AudioQueueNewInput(&fmt, jt_audio_cb, rec, NULL, kCFRunLoopCommonModes, 0, &rec->queue);
	if (err != noErr) {
		pthread_mutex_destroy(&rec->mu);
		free(rec);
		return (int)err;
	}

	if (device_uid != NULL && device_uid[0] != '\0') {
		CFStringRef uid = CFStringCreateWithCString(NULL, device_uid, kCFStringEncodingUTF8);
		if (uid == NULL) {
			err = kAudio_ParamError;
		} else {
			err = AudioQueueSetProperty(rec->queue, kAudioQueueProperty_CurrentDevice, &uid, sizeof(uid));
			CFRelease(uid);
		}
		if (err != noErr) {
			AudioQueueDispose(rec->queue, true);
			pthread_mutex_destroy(&rec->mu);
			free(rec);
			return (int)err;
		}
	}

	for (int i = 0; i < JT_BUFFER_COUNT; i++) {
		err = AudioQueueAllocateBuffer(rec->queue, JT_BUFFER_BYTES, &rec->buffers[i]);
		if (err != noErr) {
			AudioQueueDispose(rec->queue, true);
			pthread_mutex_destroy(&rec->mu);
			free(rec);
			return (int)err;
		}
		err = AudioQueueEnqueueBuffer(rec->queue, rec->buffers[i], 0, NULL);
		if (err != noErr) {
			AudioQueueDispose(rec->queue, true);
			pthread_mutex_destroy(&rec->mu);
			free(rec);
			return (int)err;
		}
	}

	err = AudioQueueStart(rec->queue, NULL);
	if (err != noErr) {
		AudioQueueDispose(rec->queue, true);
		pthread_mutex_destroy(&rec->mu);
		free(rec);
		return (int)err;
	}

	*out_recorder = rec;
	return 0;
}

void jt_audio_stop(jt_audio_recorder_t *rec) {
	if (rec == NULL) return;
	pthread_mutex_lock(&rec->mu);
	rec->running = 0;
	int fd = rec->write_fd;
	rec->write_fd = -1;
	pthread_mutex_unlock(&rec->mu);

	if (rec->queue != NULL) {
		AudioQueueStop(rec->queue, true);
		AudioQueueDispose(rec->queue, true);
	}
	if (fd >= 0) {
		close(fd);
	}
	pthread_mutex_destroy(&rec->mu);
	free(rec);
}
