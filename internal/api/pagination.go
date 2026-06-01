package api

import (
	"net/http"
	"strconv"
)

func ParsePagination(r *http.Request) (limit, offset int) {
	defaultLimit := 20

	limitString := r.URL.Query().Get("limit")
	offsetString := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitString)

	if err != nil || limit == 0 || limit > 20  {
		limit = defaultLimit
	}

	offset, err = strconv.Atoi(offsetString)
	if err != nil || offset < 0  {
		offset = 0
	}

	return limit, offset
}
