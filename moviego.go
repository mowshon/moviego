package moviego

import (
    "errors"
    "fmt"
    "github.com/tidwall/gjson"
    ffmpeg "github.com/u2takey/ffmpeg-go"
    "io/ioutil"
    "log"
    "os"
    "path/filepath"
    "strings"
)

type Video struct {
    filePath    string
    width       int64
    height      int64
    duration    float64
    probe       string
    stream      *ffmpeg.Stream
    ffmpegArgs  map[string][]string
    isTemp      bool
    hasModified bool
    extension   string
}

type Output struct {
    video Video
}

func (OutputProcess Output) Run() error {
    return OutputProcess.video.render()
}

func (V *Video) addKwArgs(key, value string) {
    if V.ffmpegArgs == nil {
        V.ffmpegArgs = make(map[string][]string)
        V.addDefaultKwArgs()
    }

    V.ffmpegArgs[key] = append(V.ffmpegArgs[key], value)
}

func (V *Video) addDefaultKwArgs() {
    //keys := Keys(V.ffmpegArgs)
    //
    //if InArray("c:v", keys) == false {
    //    V.addKwArgs("c:v", "copy")
    //}
    //
    //if InArray("c:a", keys) == false {
    //    V.addKwArgs("c:a", "copy")
    //}
}

func (V Video) ResizeByWidth(requiredWidth int64) Video {
    // New Video Height = (Current Height / Current Width ) * My Required Width
    V.height = int64((float64(V.height) / float64(V.width)) * float64(requiredWidth))

    if V.height%2 != 0 {
        V.height += 1
    }

    V.width = requiredWidth
    V.hasModified = true

    return V
}

func (V Video) ResizeByHeight(requiredHeight int64) Video {
    // New Width = (Current Width / Current Height) * My Required Height
    V.width = int64((float64(V.width) / float64(V.height)) * float64(requiredHeight))

    if V.width%2 != 0 {
        V.width += 1
    }

    V.height = requiredHeight
    V.hasModified = true

    return V
}

func (V Video) Resize(width, height int64) Video {
    V.width, V.height = width, height
    V.hasModified = true
    return V
}

func (V Video) prepareKwArgs(ignoreThisKeywords []string) ffmpeg.KwArgs {
    // Fix: ffmpeg width or height not divisible by 2 error
    if V.width%2 != 0 || V.height%2 != 0 {
        V.addKwArgs("vf", fmt.Sprintf("format=yuv444p,scale=%d:%d", V.width, V.height))
        V.hasModified = true
    } else {
        V.addKwArgs("vf", fmt.Sprintf("scale=%d:%d", V.width, V.height))
    }

    compileKwArgs := make(ffmpeg.KwArgs)
    for Keyword, Args := range V.ffmpegArgs {
        if InArray(Keyword, ignoreThisKeywords) {
            continue
        }

        compileKwArgs[Keyword] = strings.Join(Args, ",")
    }

    return compileKwArgs
}

func (V Video) Output(OutputFilename string) Output {
    compileKwArgs := V.prepareKwArgs([]string{""})

    V.stream = V.stream.Output(OutputFilename, compileKwArgs)

    return Output{video: V}
}

func (V Video) render() error {
    if V.isTemp {
        defer os.Remove(V.filePath)
    }

    return V.stream.OverWriteOutput().Run()
}

func (V *Video) checkStartAndEnd(start, end float64) {
    if start > end {
        panic("The `start` of the clip can't be bigger than its `end`.")
    }

    if start > V.duration {
        panic("The `start` cannot be bigger than the length of the main video.")
    }

    if end > V.duration {
        panic("The `end` cannot be bigger than the length of the main video.")
    }
}

func (V Video) tempRender() Video {
    tempFolder := ""
    file, err := ioutil.TempFile(tempFolder, fmt.Sprintf("video-*.%s", V.extension))

    if err != nil {
        log.Fatal(err)
    }

    renderError := V.Output(file.Name()).Run()

    if renderError != nil {
        log.Fatal(renderError)
    }

    tempVideo, loadError := Load(file.Name())

    if loadError != nil {
        log.Fatal(loadError)
    }

    tempVideo.isTemp = true

    return tempVideo
}

func (V Video) SubClip(start, end float64) Video {
    V.checkStartAndEnd(start, end)

    V.addKwArgs("ss", fmt.Sprintf("%f", start))
    V.addKwArgs("to", fmt.Sprintf("%f", end))
    V.duration = end - start
    V.hasModified = true

    return V.tempRender()
}

func Concat(videos []Video) (Video, error) {
    var videoParts string
    tempFolder := ""

    concatStorage, _ := ioutil.TempFile(tempFolder, "list-*.txt")

    var lastExtension string
    for num, video := range videos {
        lastExtension = video.extension
        
        if video.hasModified == false {
            videoParts += fmt.Sprintf("file '%s'\n", video.filePath)
        } else {
            file, tempFileErr := ioutil.TempFile(tempFolder, fmt.Sprintf("video-%d-*.%s", num, video.extension))

            if tempFileErr != nil {
                return Video{}, tempFileErr
            }

            videoParts += fmt.Sprintf("file '%s'\n", file.Name())

            video.Output(file.Name()).Run()
            fmt.Println("Done:", file.Name())

            defer os.Remove(file.Name())
        }
    }

    _, writingError := concatStorage.WriteString(videoParts)
    if writingError != nil {
        return Video{}, writingError
    }

    finalFile, finalErr := ioutil.TempFile(tempFolder, fmt.Sprintf("final-*.%s", lastExtension))
    if finalErr != nil {
        return Video{}, finalErr
    }

    err := ffmpeg.Input(concatStorage.Name(), ffmpeg.KwArgs{"f": "concat", "safe": "0"}).Output(
        finalFile.Name(), ffmpeg.KwArgs{"c": "copy"},
    ).OverWriteOutput().Run()

    if err != nil {
        return Video{}, err
    }

    defer os.Remove(concatStorage.Name())

    loadedFinalVideo, finalLoadError := Load(finalFile.Name())

    if finalLoadError != nil {
        return loadedFinalVideo, finalErr
    }

    loadedFinalVideo.isTemp = true

    return loadedFinalVideo, nil
}

func (V Video) FadeIn(start, duration float64) Video {
    V.addKwArgs("vf", fmt.Sprintf("fade=t=in:st=%.3f:d=%.3f", start, duration))
    V.hasModified = true
    return V
}

func (V Video) FadeOut(duration float64) Video {
    end := V.duration - duration
    V.addKwArgs("vf", fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", end, duration))
    V.hasModified = true
    return V
}

func (V Video) AudioFadeIn(start, duration float64) Video {
    V.addKwArgs("af", fmt.Sprintf("afade=t=in:st=%.3f:d=%.3f", start, duration))
    V.hasModified = true
    return V
}

func (V Video) AudioFadeOut(duration float64) Video {
    end := V.duration - duration
    V.addKwArgs("af", fmt.Sprintf("afade=t=out:st=%.3f:d=%.3f", end, duration))
    V.hasModified = true
    return V
}

func (V Video) Screenshot(timeInSeconds float64, outputFilename string) (string, error) {
    compileKwArgs := V.prepareKwArgs([]string{"c:a", "c:v", "af"})
    compileKwArgs["ss"] = timeInSeconds
    compileKwArgs["vframes"] = "1"

    abs, absError := filepath.Abs(outputFilename)
    if absError != nil {
        return "", absError
    }

    err := V.stream.Output(abs, compileKwArgs).OverWriteOutput().Run()
    if err != nil {
        return "", err
    }

    return abs, nil
}

func (V Video) GetFilename() string {
    return V.filePath
}

func Load(fileName string) (Video, error) {
    file, err := os.Stat(fileName)
    if errors.Is(err, os.ErrNotExist) && !file.IsDir() {
        return Video{}, os.ErrNotExist
    }

    extension := strings.Trim(filepath.Ext(fileName), ".")
    if extension == "" {
        panic(fmt.Sprintf("your file '%s' does not have an extension!", fileName))
    }

    abs, absError := filepath.Abs(fileName)
    
    videoProbe, videoProbeError := ffmpeg.Probe(abs)
	
    if videoProbeError != nil {
       log.Printf("Error: %v", err)
       return Video{}, fmt.Errorf("fatal error: %w", err)
    }

    if absError != nil {
        return Video{}, absError
    }

    return Video{
        filePath:  abs,
        width:     gjson.Get(videoProbe, "streams.0.width").Int(),
        height:    gjson.Get(videoProbe, "streams.0.height").Int(),
        duration:  gjson.Get(videoProbe, "format.duration").Float(),
        probe:     videoProbe,
        stream:    ffmpeg.Input(fileName),
        extension: extension,
    }, nil
}
