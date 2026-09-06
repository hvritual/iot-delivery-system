package localbffhttp

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
)

func writeThrottleError(writer http.ResponseWriter, err error, traceID string) bool {
	var limited *locallogin.ThrottleError
	if !errors.As(err, &limited) {
		return false
	}
	seconds := int64((limited.RetryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	writer.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	writeError(writer, http.StatusTooManyRequests, "too_many_attempts", traceID)
	return true
}
