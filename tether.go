package tether

/*
   #cgo pkg-config: libgphoto2
   #include <gphoto2/gphoto2.h>
   #include <stdlib.h>
   #include <stdio.h>

   // Wait for and capture an event, then download the image to memory.
   int capture_tethered_event(Camera *camera, GPContext *context, CameraFile **file) {
       int ret;
       CameraEventType evType;
       void *eventData;
       CameraFilePath *filePath;

       // Allocate a new CameraFile to hold image data in memory
       ret = gp_file_new(file);
       if (ret < GP_OK) return ret;

       // Wait for an event from the camera
       while (1) {
           // Polling for an event from the camera with a 500ms timeout
           ret = gp_camera_wait_for_event(camera, 500, &evType, &eventData, context);
           if (ret < GP_OK) return ret;

           if (evType == GP_EVENT_FILE_ADDED) {
               filePath = (CameraFilePath *)eventData;

               // Retrieve the file data in memory
               ret = gp_camera_file_get(camera, filePath->folder, filePath->name, GP_FILE_TYPE_NORMAL, *file, context);
               if (ret < GP_OK) {
                   gp_file_free(*file);
                   return ret;
               }

               // Optionally delete the file from the camera after download
               ret = gp_camera_file_delete(camera, filePath->folder, filePath->name, context);
               if (ret < GP_OK) return ret;

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
	"fmt"
	"log/slog"
	"time"
	"unsafe"
)

var Logger *slog.Logger

func Start(ctx context.Context) <-chan []byte {
	photos := make(chan []byte, 8)
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
			if err != nil {
				Logger.Warn("camera tether failed. retrying in 1 second", slog.Any("err", err))
				time.Sleep(1 * time.Second)
			}
		}
	}()
	return photos
}

func CaptureTethered(ctx context.Context, out chan<- []byte) error {
	// Initialize libgphoto2 context
	context := C.gp_context_new()
	Logger.Debug("initialized libgphoto2 context")
	defer C.gp_context_unref(context)

	// Initialize and open the camera
	var camera *C.Camera
	errCode := C.gp_camera_new(&camera)
	if errCode < C.GP_OK {
		return fmt.Errorf("%v", C.GoString(C.gp_result_as_string(errCode)))
	}
	defer C.gp_camera_free(camera)

	errCode = C.gp_camera_init(camera, context)
	if errCode < C.GP_OK {
		d := 10 * time.Second
		time.Sleep(d)
		return fmt.Errorf("%v", C.GoString(C.gp_result_as_string(errCode)))
	}
	Logger.Debug("initialized camera")
	defer C.gp_camera_exit(camera, context)

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
			return errors.New("aborting because errors reached consecutive threshold")
		}

		var file *C.CameraFile
		errCode = C.capture_tethered_event(camera, context, &file)
		if errCode != C.GP_OK {
			consecutiveNoEvents++
			continue
		} else {
			consecutiveNoEvents = 0
		}
		Logger.Debug("received tether event")

		// Retrieve the data from the file in memory
		var data *C.char
		var size C.ulong
		errCode = C.gp_file_get_data_and_size(file, &data, &size)
		if errCode < C.GP_OK {
			return errors.New("failed to get photo")
		}
		Logger.Debug("downloaded image from tethered camera")

		// Convert data to a Go byte slice
		imageData := C.GoBytes(unsafe.Pointer(data), C.int(size))
		out <- imageData

		// Free the file memory after download
		C.gp_file_free(file)
	}
}
