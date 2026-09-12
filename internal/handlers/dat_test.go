package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestDATHandlers(t *testing.T) {
	// 1. When datSvc == nil, all endpoints return 503 Service Unavailable
	apiNil := &API{}

	t.Run("ServiceUnavailable", func(t *testing.T) {
		reqList := httptest.NewRequest(http.MethodGet, "/api/dat/list", nil)
		recList := httptest.NewRecorder()
		apiNil.DATList(recList, reqList)
		if recList.Code != http.StatusServiceUnavailable {
			t.Errorf("DATList nil: expected 503, got %d", recList.Code)
		}

		reqTags := httptest.NewRequest(http.MethodGet, "/api/dat/tags?name=geoip.dat", nil)
		recTags := httptest.NewRecorder()
		apiNil.DATListTags(recTags, reqTags)
		if recTags.Code != http.StatusServiceUnavailable {
			t.Errorf("DATListTags nil: expected 503, got %d", recTags.Code)
		}

		reqUpdate := httptest.NewRequest(http.MethodPost, "/api/dat/update", nil)
		recUpdate := httptest.NewRecorder()
		apiNil.DATUpdate(recUpdate, reqUpdate)
		if recUpdate.Code != http.StatusServiceUnavailable {
			t.Errorf("DATUpdate nil: expected 503, got %d", recUpdate.Code)
		}

		reqRollback := httptest.NewRequest(http.MethodPost, "/api/dat/rollback", nil)
		recRollback := httptest.NewRecorder()
		apiNil.DATRollback(recRollback, reqRollback)
		if recRollback.Code != http.StatusServiceUnavailable {
			t.Errorf("DATRollback nil: expected 503, got %d", recRollback.Code)
		}

		reqSearch := httptest.NewRequest(http.MethodGet, "/api/dat/search?name=geoip.dat&tag=ru", nil)
		recSearch := httptest.NewRecorder()
		apiNil.DATSearch(recSearch, reqSearch)
		if recSearch.Code != http.StatusServiceUnavailable {
			t.Errorf("DATSearch nil: expected 503, got %d", recSearch.Code)
		}
	})

	// 2. Methods Not Allowed (POST vs GET)
	api := &API{
		datSvc: services.NewDATManagerService(t.TempDir()),
	}

	t.Run("MethodsNotAllowed", func(t *testing.T) {
		reqPostList := httptest.NewRequest(http.MethodPost, "/api/dat/list", nil)
		recPostList := httptest.NewRecorder()
		api.DATList(recPostList, reqPostList)
		if recPostList.Code != http.StatusMethodNotAllowed {
			t.Errorf("DATList POST: expected 405, got %d", recPostList.Code)
		}

		reqPostTags := httptest.NewRequest(http.MethodPost, "/api/dat/tags", nil)
		recPostTags := httptest.NewRecorder()
		api.DATListTags(recPostTags, reqPostTags)
		if recPostTags.Code != http.StatusMethodNotAllowed {
			t.Errorf("DATListTags POST: expected 405, got %d", recPostTags.Code)
		}

		reqGetUpdate := httptest.NewRequest(http.MethodGet, "/api/dat/update", nil)
		recGetUpdate := httptest.NewRecorder()
		api.DATUpdate(recGetUpdate, reqGetUpdate)
		if recGetUpdate.Code != http.StatusMethodNotAllowed {
			t.Errorf("DATUpdate GET: expected 405, got %d", recGetUpdate.Code)
		}

		reqGetRollback := httptest.NewRequest(http.MethodGet, "/api/dat/rollback", nil)
		recGetRollback := httptest.NewRecorder()
		api.DATRollback(recGetRollback, reqGetRollback)
		if recGetRollback.Code != http.StatusMethodNotAllowed {
			t.Errorf("DATRollback GET: expected 405, got %d", recGetRollback.Code)
		}

		reqPostSearch := httptest.NewRequest(http.MethodPost, "/api/dat/search", nil)
		recPostSearch := httptest.NewRecorder()
		api.DATSearch(recPostSearch, reqPostSearch)
		if recPostSearch.Code != http.StatusMethodNotAllowed {
			t.Errorf("DATSearch POST: expected 405, got %d", recPostSearch.Code)
		}
	})

	// 3. DATList success
	t.Run("DATList_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/dat/list", nil)
		rec := httptest.NewRecorder()
		api.DATList(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	// 4. DATListTags validation
	t.Run("DATListTags_Validation", func(t *testing.T) {
		// Missing name param
		reqMissing := httptest.NewRequest(http.MethodGet, "/api/dat/tags", nil)
		recMissing := httptest.NewRecorder()
		api.DATListTags(recMissing, reqMissing)
		if recMissing.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing name, got %d", recMissing.Code)
		}
	})

	// 5. DATUpdate bad JSON
	t.Run("DATUpdate_BadJSON", func(t *testing.T) {
		reqBad := httptest.NewRequest(http.MethodPost, "/api/dat/update", bytes.NewReader([]byte("{invalid")))
		reqBad.Header.Set("Content-Type", "application/json")
		recBad := httptest.NewRecorder()
		api.DATUpdate(recBad, reqBad)
		if recBad.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for bad JSON, got %d", recBad.Code)
		}
	})

	// 6. DATSearch parameter validation
	t.Run("DATSearch_Validation", func(t *testing.T) {
		// Missing both
		req1 := httptest.NewRequest(http.MethodGet, "/api/dat/search", nil)
		rec1 := httptest.NewRecorder()
		api.DATSearch(rec1, req1)
		if rec1.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing name, got %d", rec1.Code)
		}

		// Missing tag
		req2 := httptest.NewRequest(http.MethodGet, "/api/dat/search?name=geoip.dat", nil)
		rec2 := httptest.NewRecorder()
		api.DATSearch(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing tag, got %d", rec2.Code)
		}
	})
}
