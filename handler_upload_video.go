package main

import (
	"os"
	"io"
	"fmt"
	"time"
	"mime"
	"bytes"
	"strings"
	"os/exec"
	"context"
	"net/http"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
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

	aws_fn, err := getVideoAspectRatio(new_file.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't determine prefix", err)
		return
	}

	last_fn := aws_fn + file_name

	new_fp, err := processVideoForFastStart(new_file.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create processed file", err)
		return
	}

	new_file.Seek(0, io.SeekStart)

	n_file, err := os.Open(new_fp)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open processed file's path", err)
		return
	}
	defer n_file.Close()

	put_params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &last_fn,
		Body:        n_file,
		ContentType: &mt_type,
	}

	_, err = cfg.s3Client.PutObject(context.Background(), &put_params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload the video to bucket", err)
		return
	}

	new_url := fmt.Sprintf("%v,%v", cfg.s3Bucket, last_fn)
	video.VideoURL = &new_url

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video thumbnail", err)
		return
	}

	signed_vid, err := cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create signed video url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, signed_vid)
}

type Stream struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type form_probe struct {
	Streams []Stream `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v",
		"error",
		"-print_format",
		"json",
		"-show_streams",
		filePath,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	buf := bytes.NewReader(out)

	var output form_probe
	decoder := json.NewDecoder(buf)
	if err := decoder.Decode(&output); err != nil {
		return "", err
	}

	var width int
	var height int
	for _, stream := range output.Streams {
		if stream.Width != 0 && stream.Height != 0 {
			width = stream.Width
			height = stream.Height
			break
		}
	}

	ratio := float64(width)/float64(height)
	aspect_ratio := "other/"
	if ratio >= 1.768 && ratio <= 1.788 {
		aspect_ratio = "landscape/"
	} else if ratio >= 0.553 && ratio <= 0.573 {
		aspect_ratio = "portrait/"
	}

	return aspect_ratio, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	out_fp := filePath + ".processing"

	cmd := exec.Command(
		"ffmpeg",
		"-i",
		filePath,
		"-c",
		"copy",
		"-movflags",
		"faststart",
		"-f",
		"mp4",
		out_fp,
	)

	_, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	return out_fp, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	new_client := s3.NewPresignClient(s3Client)

	params := s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}

	req, err := new_client.PresignGetObject(context.Background(), &params, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return req.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil || len(*video.VideoURL) == 0 {
		return video, nil
	}

	splitted := strings.SplitN(*video.VideoURL, ",", 2)
	if len(splitted) != 2 {
		return video, fmt.Errorf("Invalid Video URL format")
	}

	bucket, key := splitted[0], splitted[1]

	url, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Duration(time.Minute * 10))
	if err != nil {
		return video, err
	}

	video.VideoURL = &url

	return video, nil
}
