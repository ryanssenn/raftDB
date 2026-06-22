import { createLayers, Renderer } from "./renderer.js";
import { AnimationEngine } from "./animation.js";
import { EventStream } from "./events.js";
import { Controls } from "./controls.js";

const layers = createLayers();
const renderer = new Renderer(layers);
const anim = new AnimationEngine(layers, renderer);

const stream = new EventStream(renderer, anim, (state) => controls.onState(state));
const controls = new Controls(stream);

stream.connect();

function loop(now) {
  anim.tick(now);
  requestAnimationFrame(loop);
}
requestAnimationFrame(loop);

// Auto-configure default cluster in sandbox mode
fetch("/api/cluster/status")
  .then((r) => r.json())
  .then((data) => {
    if (!data.clusterStarted && data.nodeCount) {
      document.getElementById("node-count").value = data.nodeCount;
      document.getElementById("node-count-val").textContent = data.nodeCount;
    }
  })
  .catch(() => {});
