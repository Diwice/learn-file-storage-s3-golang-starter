package main

import (
	"os"
	"io"
	"fmt"
	"mime"
	"context"
	"net/http"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1 << 30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't validate JWT", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find the video's metadata", err)
		return
	} else if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of the video", fmt.Errorf("Wrong ownership"))
		return
	}

	file, f_header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read the file", err)
		return
	}
	defer file.Close()

	m_type := f_header.Header.Get("Content-Type")
	if m_type == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type", fmt.Errorf("No Content-Type"))
		return
	}

	mt_type, _, err := mime.ParseMediaType(m_type)
	if mt_type != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Wrong file extension", fmt.Errorf("Wrong file extension"))
		return
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't parse media type", err)
		return
	}

	file_ext := ".mp4"
	file_name := createRandFilename()+file_ext

	new_file, err := os.CreateTemp("", file_name)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}
	defer os.Remove(new_file.Name())
	defer new_file.Close()

	_, err = io.Copy(new_file, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't copy the file", err)
		return
	}

	new_file.Seek(0, io.SeekStart)

	put_params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &file_name,
		Body:        new_file,
		ContentType: &mt_type,
	}

	_, err = cfg.s3Client.PutObject(context.Background(), &put_params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload the video to bucket", err)
		return
	}

	new_url := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, file_name)
	video.VideoURL = &new_url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video thumbnail", err)
		return
	}

	formatted_vid, err := json.Marshal(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't marshal the return vals", err)
		return
	}

	respondWithJSON(w, http.StatusOK, formatted_vid)
}
