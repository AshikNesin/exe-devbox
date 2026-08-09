# devbox Remotion demo

The published demo lives at [`../media/devbox-demo.mp4`](../media/devbox-demo.mp4) and is embedded in the main README. The Remotion render output goes to `devbox-demo/out/devbox-demo.mp4`.

## Render locally

```bash
cd videos/devbox-demo
npm install
npm run render

# publish the render
cp out/devbox-demo.mp4 ../../media/devbox-demo.mp4

# regenerate the GIF embedded in the README (GitHub only inline-renders images,
# so the mp4 is mirrored as a GIF; requires ffmpeg)
ffmpeg -y -v error -i ../../media/devbox-demo.mp4 \
  -vf "fps=15,scale=960:-1:flags=lanczos,palettegen=stats_mode=diff" /tmp/palette.png
ffmpeg -y -v error -i ../../media/devbox-demo.mp4 -i /tmp/palette.png \
  -lavfi "fps=15,scale=960:-1:flags=lanczos [x]; [x][1:v] paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
  ../../media/devbox-demo.gif
```

Preview the composition in Remotion Studio:

```bash
npm start
```

The 15-second video covers `devbox setup`, `new`, `dev`, and `remove`, including the guarantee that removing a project never deletes its codebase.
