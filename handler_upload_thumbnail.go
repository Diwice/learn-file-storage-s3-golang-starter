package main

import (
	"os"
	"io"
	"fmt"
	"mime"
	"bytes"
	"strings"
	"net/http"
	"crypto/rand"
	"path/filepath"
	"encoding/json"
	"encoding/base64"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	m_type := header.Header.Get("Content-Type")
	if m_type == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for the file", fmt.Errorf("No Content-Type"))
		return
	} else if !strings.Contains(m_type, "image/jpeg") && !strings.Contains(m_type, "image/png") {
		respondWithError(w, http.StatusBadRequest, "Unallowed Content-Type", fmt.Errorf("Only JPEG or PNG file types allowed"))
		return
	}

	video_md, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't fetch the video metadata", err)
		return
	} else if video_md.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of the video", err)
		return
	}

	extensions, err := mime.ExtensionsByType(m_type)
	if err != nil || len(extensions) == 0 {
		respondWithError(w, http.StatusBadRequest, "Unknown media type", fmt.Errorf("No matched extensions for media type"))
		return
	}
	file_ext := extensions[0]
	file_name := createRandFilename()
	new_path := filepath.Join(cfg.assetsRoot, file_name)

	new_file, err := os.Create(new_path+file_ext)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create the file", err)
		return
	}

	_, err = io.Copy(new_file, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't write the file", err)
		return
	}

	thumb_url := fmt.Sprintf("http://localhost:%v/assets/%v%v", cfg.port, file_name, file_ext)
	video_md.ThumbnailURL = &thumb_url

	err = cfg.db.UpdateVideo(video_md)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video thumbnail", err)
		return
	}

	formatted_md, err := json.Marshal(video_md)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't marshal the return data", err)
		return
	}

	respondWithJSON(w, http.StatusOK, formatted_md)
}

func createRandFilename() string {
	key := make([]byte, 32)
	rand.Read(key)

	res_buf := bytes.NewBuffer([]byte{})

	encoder := base64.NewEncoder(base64.RawURLEncoding, res_buf)
	encoder.Write(key)
	encoder.Close()

	return res_buf.String()
}
