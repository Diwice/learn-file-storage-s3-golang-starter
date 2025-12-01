package main

import (
	"io"
	"fmt"
	"net/http"
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
	}

	b, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read the file data", err)
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

	enc_data := base64.StdEncoding.EncodeToString(b)
	data_url := fmt.Sprintf("data:[%v][;base64],%v", m_type, enc_data)
	video_md.ThumbnailURL = &data_url

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
