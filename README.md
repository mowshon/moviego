## 📽 MovieGo - Video Editing in Golang

**MovieGo** is a Golang library for video editing. The library is designed for fast processing of routine tasks related to video editing. The main core of the project is the [ffmpeg-go](https://github.com/u2takey/ffmpeg-go) package, which simplifies working with the **ffmpeg** library.

### ⬇️ Instalation

```bash
go get github.com/mowshon/moviego
```

### 🎥 Resize video in Golang

Currently there are three methods in the package that help you resize the video:
* `ResizeByWidth( new width )`
* `ResizeByHeight( new height )`
* `Resize( new width, new height )`

```golang
package main

import (
    "github.com/mowshon/moviego"
)

func main() {
    first, _ := moviego.Load("forest.mp4")

    first.ResizeByWidth(500).Output("resized-by-width.mp4").Run()
    first.ResizeByWidth(150).Output("resized-by-height.mp4").Run()
    first.Resize(1000, 500).Output("resized.mp4").Run()
}
```

These commands in ffmpeg:

```bash
ffmpeg -i forest.mp4 -vf scale=500:210 resized-by-width.mp4 -y
ffmpeg -i forest.mp4 -vf scale=150:62 resized-by-height.mp4 -y
ffmpeg -i forest.mp4 -vf scale=1000:500 resized.mp4 -y
```

### 🎥 Cut video in Golang
The `Video` structure has a `SubClip` method which can trim the video by specifying the beginning and end of the video segment.

```golang
package main

import (
    "github.com/mowshon/moviego"
    "log"
)

func main() {
    first, _ := moviego.Load("forest.mp4")

    // Cut video from second 3 to second 5.
    err := first.SubClip(3, 5).Output("final.mp4").Run()
    if err != nil {
        log.Fatal(err)
    }
}
```

### 🎥 Combine multiple videos into one in Golang
Having several videos you can combine them into one. You can apply different effects to video clips from a slice at the same time.

```golang
func main() {
    first, _ := moviego.Load("forest.mp4")
    second, _ := moviego.Load("sky.mp4")

    // Combine multiple videos into one.
    finalVideo, err := moviego.Concat([]moviego.Video{
        first,
        second,
        first.SubClip(1, 3),
        second.SubClip(5.3, 10.5),
        first.FadeIn(0, 5).FadeOut(5),
    })

    if err != nil {
        log.Fatal(err)
    }

    renderErr := finalVideo.Output("final.mp4").Run()
    if err != nil {
        log.Fatal(renderErr)
    }
}
```

### 🎥 Add a fade-in or fade-out transition for Video and Audio

Here we have 4 methods for working with `Fade` effects. Two for video and two for audio tracks from the video.

* `.FadeIn(start, duration)` - The video fade-in from the beginning (the screen is black) to the specified time interval.
* `.FadeOut(seconds before the end)` - Fading video into a completely black screen. You need to specify in seconds from the end of the video when to start fading.
* `.AudioFadeIn(start, duration)` - If you want the audio track to be completely muted at the beginning, you can specify the beginning at 0.5 seconds.
* `.AudioFadeOut(seconds before the end)` - The audio track will fade out at the end depending on the specified interval in seconds to the end of the video.

```golang
func main() {
    first, _ := moviego.Load("forest.mp4")

    // Add fade-in and fade-out
    first.FadeIn(0, 3).FadeOut(5).Output("fade-in-with-fade-out.mp4").Run()

    // Cut video and add Fade-in
    first.SubClip(5.20, 10).FadeIn(0, 3).Output("cut-fade-in.mp4").Run()

    // Mute the sound for the first 0.5 seconds and then
    // turn the sound on with the fade in.
    first.AudioFadeIn(0.5, 4).Output("audio-fade-in.mp4").Run()

    // Add video fade-out with audio fade-out.
    first.FadeOut(5).AudioFadeOut(5).Output("fade-out.mp4").Run()
}
```

### 🖼️ Screenshot - Saving a Frame of Video Clip in Golang

You can make a screenshot by specifying the desired time from the video in seconds.

```golang
func main() {
    first, _ := moviego.Load("forest.mp4")

    // A simple screenshot from the video.
    first.Screenshot(5, "simple-screen.png")

    // Take a screenshot after applying the effects.
    first.FadeIn(0, 3).FadeOut(5).Screenshot(0.4, "screen.png")
}
```
