# devbox Remotion demo

The published demo lives at [`../media/devbox-demo.mp4`](../media/devbox-demo.mp4) and is embedded in the main README. The Remotion render output goes to `devbox-demo/out/devbox-demo.mp4`.

## Render locally

```bash
cd videos/devbox-demo
npm install
npm run render

# publish the render
cp out/devbox-demo.mp4 ../../media/devbox-demo.mp4
```

Preview the composition in Remotion Studio:

```bash
npm start
```

The 15-second video covers `devbox setup`, `new`, `dev`, and `remove`, including the guarantee that removing a project never deletes its codebase.
