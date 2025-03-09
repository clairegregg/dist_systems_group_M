import Canvas from "./Canvas/Canvas";
import useWebSocket from "./Canvas/useWebSocket";

function App() {
  const gameState = useWebSocket("ws://localhost:8082/ws");
  const draw = (context, count) => {
      context.clearRect(0,0,context.canvas.width,context.canvas.height)
      context.font = "50px Arial"
      const delta = count % 800
      context.fillText("Hello World",10 + delta, 80)
      context.fillText(JSON.stringify(gameState), 10, 100);
  }

  return <Canvas draw={draw} width="800" height="500"/>
}

export default App;