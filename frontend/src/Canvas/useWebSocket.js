// useWebSocket.js
import { useEffect, useState } from "react";

function useWebSocket(url) {
  const [gameState, setGameState] = useState({});

  useEffect(() => {
    const ws = new WebSocket(url);

    ws.onopen = () => {
      console.log("WebSocket connection opened");
      // Optionally, send an initial player state to register the player.
      const initialPlayerState = {
        id: "player2", // Ensure this is unique for each player.
        x: 0,
        y: 0,
        direction: "right"
      };
      ws.send(JSON.stringify(initialPlayerState));
    };

    ws.onmessage = (event) => {
      console.log("Received message:", event.data);
      try {
        const state = JSON.parse(event.data);
        setGameState(state);
      } catch (err) {
        console.error("Error parsing message:", err);
      }
    };

    ws.onerror = (error) => {
      console.error("WebSocket error:", error);
    };

    ws.onclose = () => {
      console.log("WebSocket connection closed");
    };

    // Cleanup function: close the WebSocket when the component unmounts.
    return () => {
      ws.close();
    };
  }, [url]);

  return gameState;
}

export default useWebSocket;