package tether

/*
   #cgo pkg-config: libgphoto2
   #include <gphoto2/gphoto2.h>
   #include <stdlib.h>
   #include <stdio.h>

   // Wait for and capture an event, then download the image to memory.
   int capture_tethered_event(Camera *camera, GPContext *context, CameraFile **file, const char **mimeType) {
		int ret;
		CameraEventType evType;
		void *eventData;
		CameraFilePath *filePath;

		ret = gp_file_new(file);
		if (ret < GP_OK) return ret;

		while (1) {
			ret = gp_camera_wait_for_event(camera, 500, &evType, &eventData, context);
			if (ret < GP_OK) return ret;

			if (evType == GP_EVENT_FILE_ADDED) {
				filePath = (CameraFilePath *)eventData;

				ret = gp_camera_file_get(camera, filePath->folder, filePath->name, GP_FILE_TYPE_NORMAL, *file, context);
				if (ret < GP_OK) {
					gp_file_free(*file);
					return ret;
				}

				// Retrieve MIME type
				ret = gp_file_get_mime_type(*file, mimeType);
				if (ret < GP_OK) {
					gp_file_free(*file);
					return ret;
				}

				return GP_OK;
			}
		}
		return GP_OK;
}

   // Error handling utility function
   void check_error(int err_code) {
       if (err_code < GP_OK) {
           fprintf(stderr, "GPhoto2 error: %s\n", gp_result_as_string(err_code));
           exit(1);
       }
   }
*/
import "C"
import (
	"context"
	"errors"
	"log/slog"
	"time"
	"unsafe"
)

var Logger *slog.Logger

var (
	FailedToGetDataErr    = errors.New("gp_file_get_data_and_size failed")
	FailedToOpenCameraErr = errors.New("gp_camera_new failed")
	CameraInitFailedErr   = errors.New("gp_camera_init failed")
	CameraTetherFailedErr = errors.New("tether failed")
)

type Capture struct {
	Data     []byte
	MimeType string
}

func Start(ctx context.Context) <-chan Capture {
	photos := make(chan Capture, 32)
	go func() {
		defer close(photos)
		defer slog.Debug("tether stopped")
		for {
			select {
			case <-ctx.Done():
				Logger.Debug("tether will stop because context was closed")
				return
			default:
			}

			err := CaptureTethered(ctx, photos)
			switch err {
			case nil:
				break
			case CameraInitFailedErr:
				Logger.Warn("camera tether failed. retrying in 10 second", slog.Any("err", err))
				time.Sleep(10 * time.Second)
				break
			default:
				Logger.Warn("camera tether failed. retrying in 1 second", slog.Any("err", err))
				time.Sleep(1 * time.Second)
			}
		}
	}()
	return photos
}

func CaptureTethered(ctx context.Context, out chan<- Capture) error {
	// Initialize libgphoto2 context
	context := C.gp_context_new()
	Logger.Debug("initialized libgphoto2 context")
	defer C.gp_context_unref(context)

	// Initialize and open the camera
	var camera *C.Camera
	errCode := C.gp_camera_new(&camera)
	if errCode < C.GP_OK {
		Logger.Debug("gp_camera_new failed", slog.String("err", C.GoString(C.gp_result_as_string(errCode))))
		return FailedToOpenCameraErr
	}
	defer C.gp_camera_free(camera)

	errCode = C.gp_camera_init(camera, context)
	if errCode < C.GP_OK {
		//d := 10 * time.Second
		//time.Sleep(d)
		Logger.Debug("gp_camera_init failed", slog.String("err", C.GoString(C.gp_result_as_string(errCode))))
		return CameraInitFailedErr
	}
	Logger.Debug("initialized camera")
	defer func() {
		C.gp_camera_exit(camera, context)
		Logger.Debug("camera exited")
	}()

	// Tethered capture loop
	var consecutiveNoEvents int
	for {
		select {
		case <-ctx.Done():
			Logger.Debug("tether will stop because context was closed")
			return ctx.Err()
		default:
		}
		if consecutiveNoEvents > 10 {
			Logger.Debug("aborting because errors reached consecutive threshold")
			return CameraTetherFailedErr
		}

		var file *C.CameraFile
		var mimeType *C.char
		errCode = C.capture_tethered_event(camera, context, &file, (**C.char)(unsafe.Pointer(&mimeType)))
		if errCode != C.GP_OK {
			consecutiveNoEvents++
			continue
		} else {
			consecutiveNoEvents = 0
		}
		mimeTypeStr := C.GoString(mimeType)
		Logger.Debug("received tether event")

		// Retrieve the data from the file in memory
		var data *C.char
		var size C.ulong
		errCode = C.gp_file_get_data_and_size(file, &data, &size)
		if errCode < C.GP_OK {
			Logger.Debug("gp_file_get_data_and_size failed", slog.Any("err", errCode))
			return FailedToGetDataErr
		}
		Logger.Debug("downloaded image from tethered camera")

		// Convert data to a Go byte slice
		imageData := C.GoBytes(unsafe.Pointer(data), C.int(size))
		out <- Capture{
			Data:     imageData,
			MimeType: mimeTypeStr,
		}

		// Free the file memory after download
		C.gp_file_free(file)
	}
}
