import React, { useEffect, useRef, useState } from "react";
import GlobalLeaderboard from "./GlobalLeaderboard";
import map from "./maps"


function App() {
  const WS_URL = "ws://localhost:8082/ws";
  const CHUCK_URL = "http://localhost:8082/getMap"
  const CENTRAL_URL = "http://localhost:8080/"
  // Refs for canvas and scoreboard elements.
  const canvasRef = useRef(null);
  const scoreElRef = useRef(null);
  const scoreboardRef = useRef(null);
  // Use sessionStorage so that the player ID persists across refreshes but clears when the tab is closed.
  const storedPlayer = sessionStorage.getItem("localPlayer");
  const initialPlayer = storedPlayer ? JSON.parse(storedPlayer) : null;
  const maps = []
  for (let item in map){
    maps.push(map[item])
  }
  // If an ID already exists, use it; otherwise, generate a new one.
  const localPlayerIdRef = useRef(
    initialPlayer?.id ||
    Date.now().toString() + Math.random().toString(36).substring(2)
  );
  
  // gridOffsetRef translates game world coordinates into screen coordinates.
  const gridOffsetRef = useRef({ x: 0, y: 0 });
  
  // State for map and loaded flag.
  const [localMap, setLocalMap] = useState([]);
  const [mapLoaded, setMapLoaded] = useState(false);
  const mapLoadedRef = useRef(mapLoaded);
  useEffect(() => {
    mapLoadedRef.current = mapLoaded;
  }, [mapLoaded]);
  
  // Persist local player data in sessionStorage.
  function saveLocalPlayer(player) {
    sessionStorage.setItem("localPlayer", JSON.stringify(player));
  }
  
  useEffect(() => {
    // Do NOT clear sessionStorage on beforeunload; this allows the ID to persist through refresh.
    // Only the WebSocket connection will be closed on unload.
    
    let mapX = 1
    let mapY = 1
    const canvas = canvasRef.current;
    const scoreEl = scoreElRef.current;
    const scoreboardEl = scoreboardRef.current;
    const c = canvas.getContext("2d");

    // Set initial canvas dimensions.
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;

    let remoteMessageQueue = [];
    const CELL_SIZE = 40; // game world cell size

    // --- Classes using game world coordinates ---
    class Boundary {
      constructor({ gamePos }) {
        this.gamePos = gamePos;
        this.width = CELL_SIZE;
        this.height = CELL_SIZE;
      }
      draw() {
        const screenX = gridOffsetRef.current.x + this.gamePos.x;
        const screenY = gridOffsetRef.current.y + this.gamePos.y;
        c.fillStyle = "blue";
        c.fillRect(screenX, screenY, this.width, this.height);
      }
    }

    class Player {
      constructor({ id, gamePos, velocity, color, isLocal, score = 0,X,Y }) {
        this.id = id;
        this.gamePos = { ...gamePos };
        this.velocity = velocity;
        this.radius = 15;
        this.color = color;
        this.isLocal = isLocal;
        this.score = score;
        this.targetGamePos = { ...gamePos };
        this.lastUpdate = Date.now();
        this.X = X;
        this.Y = Y;
      }
      update(delta) {
        if (!this.isLocal) {
          const dx = this.targetGamePos.x - this.gamePos.x;
          const dy = this.targetGamePos.y - this.gamePos.y;
          const snapThreshold = 5000;
          if (Math.abs(dx) > snapThreshold || Math.abs(dy) > snapThreshold) {
            this.gamePos = { ...this.targetGamePos };
          } else {
            const smoothingFactor = 20;
            this.gamePos.x += dx * delta * smoothingFactor;
            this.gamePos.y += dy * delta * smoothingFactor;
          }
        }
        console.log(`Player: ${this.id}, X:${this.X}, Y:${this.Y}`)
        if(this.X === mapX && this.Y === mapY){
          this.draw();
        }
      }
      draw() {
        const screenX = gridOffsetRef.current.x + this.gamePos.x;
        const screenY = gridOffsetRef.current.y + this.gamePos.y;
        c.beginPath();
        c.arc(screenX, screenY, this.radius, 0, Math.PI * 2);
        c.fillStyle = this.color;
        c.fill();
        c.closePath();
      }
    }

    class Pellet {
      constructor({ gamePos }) {
        this.gamePos = gamePos;
        this.radius = 3;
        this.id = `${gamePos.x}-${gamePos.y}`;
      }
      draw() {
        const screenX = gridOffsetRef.current.x + this.gamePos.x;
        const screenY = gridOffsetRef.current.y + this.gamePos.y;
        c.beginPath();
        c.arc(screenX, screenY, this.radius, 0, Math.PI * 2);
        c.fillStyle = "white";
        c.fill();
        c.closePath();
      }
    }

    // --- Collections for game objects ---
    const remotePlayers = new Map();
    const pellets = [];
    const boundaries = [];

    // Local player.
    const localPlayerGamePos = { x: CELL_SIZE * 1.5, y: CELL_SIZE * 1.5 };
    const localPlayerId = localPlayerIdRef.current;
    const localPlayerColor =
      initialPlayer?.color || `hsl(${Math.random() * 360}, 70%, 60%)`;
    const localPlayer = new Player({
      id: localPlayerId,
      gamePos: { ...localPlayerGamePos },
      velocity: { x: 0, y: 0 },
      color: localPlayerColor,
      isLocal: true,
      score: initialPlayer?.score || 0,
      X:initialPlayer?.X || mapX,
      Y:initialPlayer?.Y || mapY
    });
    saveLocalPlayer({
      id: localPlayer.id,
      color: localPlayer.color,
      score: localPlayer.score,
      X:mapX,
      Y:mapY
    });

    const keys = {
      w: { pressed: false },
      a: { pressed: false },
      s: { pressed: false },
      d: { pressed: false },
    };

    // --- WebSocket Setup ---
    const socket = new WebSocket(WS_URL);
    const handleBeforeUnload = () => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "disconnect", id: localPlayer.id }));
      }
    };
    // Process remote combined state messages.
    function processRemoteMessage(messageData) {
      try {
        const data = JSON.parse(messageData);
        // Update remote players.
        if (data.players) {
          Object.entries(data.players).forEach(([id, playerData]) => {
            if (id === localPlayer.id) {
              // Optionally ignore remote updates for the local player.
              return;
            }
            const gamePos = { ...playerData.position };
            console.log(playerData)
            if (!remotePlayers.has(id)) {
              remotePlayers.set(
                id,
                new Player({
                  id,
                  gamePos,
                  velocity: playerData.velocity,
                  color:
                    playerData.color ||
                    `hsl(${Math.random() * 360}, 70%, 60%)`,
                  isLocal: false,
                  score: playerData.score || 0,
                  X: playerData.location.x,
                  Y: playerData.location.y,
                })
              );
            } else {
              const remotePlayer = remotePlayers.get(id);
              remotePlayer.lastUpdate = Date.now();
              if (
                Math.abs(gamePos.x - remotePlayer.targetGamePos.x) > 5 ||
                Math.abs(gamePos.y - remotePlayer.targetGamePos.y) > 5
              ) {
                remotePlayer.targetGamePos = { ...gamePos };
              }
              remotePlayer.velocity = playerData.velocity;
              remotePlayer.score = playerData.score || remotePlayer.score;
              remotePlayer.X = playerData.location.x
              remotePlayer.Y = playerData.location.y
            }
          });
          updateScoreboard();
        }
    
        // Process the eatenPellets array (IDs of pellets to delete).
        if (data.eatenPellets && Array.isArray(data.eatenPellets)) {
          data.eatenPellets.forEach((pelletId) => {
            for (let i = pellets.length - 1; i >= 0; i--) {
              if (pellets[i].id === pelletId) {
                pellets.splice(i, 1);
                break;
              }
            }
          });
        }
      } catch (err) {
        console.error("Error processing remote message:", err, messageData);
      }
    }

    socket.addEventListener("message", (event) => {
      if (!mapLoadedRef.current) {
        remoteMessageQueue.push(event.data);
      } else {
        processRemoteMessage(event.data);
      }
    });

    socket.addEventListener("open", () => {
      console.log("WebSocket connected");
    });

    // Close WebSocket only when the tab is closed.
    window.addEventListener("beforeunload", () => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(
          JSON.stringify({ type: "disconnect", id: localPlayer.id })
        );
        socket.close();
      }
    });

    let lastScoreboardHTML = "";
    function updateScoreboard() {
      let html = `<h3>Players</h3><ul>`;
      html += `<li>${localPlayer.id} (You): ${localPlayer.score}</li>`;
      remotePlayers.forEach((player) => {
        html += `<li>${player.id}: ${player.score}</li>`;
      });
      html += `</ul>`;
      
      if (scoreboardRef.current && html !== lastScoreboardHTML) {
        scoreboardRef.current.innerHTML = html;
        lastScoreboardHTML = html;
      }
    }

    function collisionDetection({ gamePos, boundary }) {
      return (
        gamePos.y - localPlayer.radius <= boundary.gamePos.y + boundary.height &&
        gamePos.x + localPlayer.radius >= boundary.gamePos.x &&
        gamePos.y + localPlayer.radius >= boundary.gamePos.y &&
        gamePos.x - localPlayer.radius <= boundary.gamePos.x + boundary.width
      );
    }

    async function loadMap() {
      try {
        // const res = await fetch(CHUCK_URL);
        // if (!res.ok) {
        //   console.error("Failed to fetch map, status:", res.status);
        //   return;
        // }
        // const map = await res.json();
        setLocalMap(maps[mapX*4+mapY]);
        const rows = maps[0].length;
        const cols = maps[0][0].length;
        const gridWidth = cols * CELL_SIZE;
        const gridHeight = rows * CELL_SIZE;
        const offsetX = (canvas.width - gridWidth) / 2;
        const offsetY = (canvas.height - gridHeight) / 2;
        gridOffsetRef.current = { x: offsetX, y: offsetY };

        boundaries.length = 0;
        pellets.length = 0;
        maps[mapX*4+mapY].forEach((row, i) => {
          row.forEach((symbol, j) => {
            if (symbol === "1") {
              boundaries.push(
                new Boundary({ gamePos: { x: j * CELL_SIZE, y: i * CELL_SIZE } })
              );
            } else if (symbol === "0") {
              pellets.push(
                new Pellet({
                  gamePos: {
                    x: j * CELL_SIZE + CELL_SIZE / 2,
                    y: i * CELL_SIZE + CELL_SIZE / 2,
                  },
                })
              );
            }
          });
        });

        setMapLoaded(true);
        remoteMessageQueue.forEach((msg) => processRemoteMessage(msg));
        remoteMessageQueue = [];

        // Now send the new player message using the envelope format.
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(
            JSON.stringify({
              type: "player",
              data: {
                id: localPlayer.id,
                color: localPlayer.color,
                score: localPlayer.score,
                position: { ...localPlayer.gamePos },
                velocity: localPlayer.velocity,
                location:{
                  X:mapX,
                  Y:mapY
                },
              },
            })
          );
        }
      } catch (err) {
        console.error("Error loading map:", err);
      }
    }

    async function reloadMap() {
      setMapLoaded(false);
      boundaries.length = 0;
      pellets.length = 0;
      await loadMap();
      console.log("Map reloaded");
    }

    async function swapMap(newX,newY) {
      boundaries.length = 0;
      pellets.length = 0;
      localPlayer.X = newX;
      localPlayer.Y = newY;
      mapX = newX;
      mapY = newY;
      await loadMap();
      console.log("Swapped Map")
    }

    function sendPlayerUpdate() {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(
          JSON.stringify({
            type: "player",
            data: {
              id: localPlayer.id,
              position: { ...localPlayer.gamePos },
              velocity: localPlayer.velocity,
              color: localPlayer.color,
              score: localPlayer.score,
              location:{
                X:mapX,
                Y:mapY
              },
            },
          })
        );
      }
    }

    const handleKeyDown = ({ key }) => {
      if (keys[key]) {
        if (!keys[key].pressed) {
          keys[key].pressed = true;
          sendPlayerUpdate();
        }
      }
    };
    const handleKeyUp = ({ key }) => {
      if (keys[key]) {
        keys[key].pressed = false;
        sendPlayerUpdate();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    window.addEventListener("keyup", handleKeyUp);

    const handleResize = () => {
      canvas.width = window.innerWidth;
      canvas.height = window.innerHeight;
      reloadMap();
    };
    window.addEventListener("resize", handleResize);

    window.reloadMap = reloadMap;

    let animationFrameId;
    let lastFrameTime = performance.now();
    let previousGamePos = { ...localPlayer.gamePos };

    function animate() {
      animationFrameId = requestAnimationFrame(animate);
      if (localPlayer.gamePos.x < 0){
        localPlayer.gamePos.x = 650
        swapMap(mapX+1,mapY)
      }
      else if (localPlayer.gamePos.x > 650){
        localPlayer.gamePos.x = 0
        swapMap(mapX-1,mapY)
      }
      else if(localPlayer.gamePos.y < 0 ){
        localPlayer.gamePos.y = 650
        swapMap(mapX,mapY+1)
      }
      else if(localPlayer.gamePos.y > 650 ){
        localPlayer.gamePos.y = 0
        swapMap(mapX,mapY-1)
      }
      const now = performance.now();
      const rawDelta = (now - lastFrameTime) / 1000;
      const delta = Math.min(rawDelta, 0.05);
      lastFrameTime = now;
      c.clearRect(0, 0, canvas.width, canvas.height);

      let vx = localPlayer.velocity.x;
      let vy = localPlayer.velocity.y;
      if (keys.w.pressed) vy = -6;
      if (keys.s.pressed) vy = 6;
      if (keys.a.pressed) vx = -6;
      if (keys.d.pressed) vx = 6;

      const newGamePos = {
        x: localPlayer.gamePos.x + vx,
        y: localPlayer.gamePos.y + vy,
      };

      let blockedX = false;
      let blockedY = false;
      for (const boundary of boundaries) {
        if (
          collisionDetection({
            gamePos: { x: newGamePos.x, y: localPlayer.gamePos.y },
            boundary,
          })
        )
          blockedX = true;
        if (
          collisionDetection({
            gamePos: { x: localPlayer.gamePos.x, y: newGamePos.y },
            boundary,
          })
        )
          blockedY = true;
      }
      localPlayer.velocity.x = blockedX ? 0 : vx;
      localPlayer.velocity.y = blockedY ? 0 : vy;
      localPlayer.gamePos.x += localPlayer.velocity.x;
      localPlayer.gamePos.y += localPlayer.velocity.y;

      const posChanged =
        Math.abs(localPlayer.gamePos.x - previousGamePos.x) > 5 ||
        Math.abs(localPlayer.gamePos.y - previousGamePos.y) > 5;
      if (posChanged) {
        sendPlayerUpdate();
        previousGamePos = { ...localPlayer.gamePos };
      }

      for (let i = pellets.length - 1; i >= 0; i--) {
        const pellet = pellets[i];
        if (
          Math.hypot(
            pellet.gamePos.x - localPlayer.gamePos.x,
            pellet.gamePos.y - localPlayer.gamePos.y
          ) < pellet.radius + localPlayer.radius
        ) {
          pellets.splice(i, 1);
          localPlayer.score += 10;
          if (scoreEl) scoreEl.innerHTML = localPlayer.score;
          updateScoreboard();
          if (socket.readyState === WebSocket.OPEN) {
            socket.send(
              JSON.stringify({
                type: "pellet",
                data: {
                  pelletId: pellet.id,
                  id: localPlayer.id,
                  score: localPlayer.score,
                  location:{
                    X: mapX,
                    Y: mapY
                  },
                },
              })
            );
          }
        }
      }

      boundaries.forEach((b) => b.draw());
      pellets.forEach((p) => p.draw());
      localPlayer.draw();

      const currentTime = Date.now();
      for (const [id, player] of remotePlayers) {
        if (currentTime - player.lastUpdate > 3000) {
          remotePlayers.delete(id);
        }
      }
      remotePlayers.forEach((player) => {
        player.update(delta);
      });
    }

    Promise.all([
      new Promise((resolve) => {
        if (socket.readyState === WebSocket.OPEN) {
          resolve();
        } else {
          socket.addEventListener("open", resolve);
        }
      }),
      loadMap(),
    ]).then(() => {
      animate();
    });

    return () => {
      cancelAnimationFrame(animationFrameId);
      window.removeEventListener("keydown", handleKeyDown);
      window.removeEventListener("keyup", handleKeyUp);
      window.removeEventListener("resize", handleResize);
      window.removeEventListener("beforeunload", handleBeforeUnload);
      if (socket.readyState === WebSocket.OPEN) {
        socket.close();
      }
      delete window.reloadMap;
    };
  }, []);

  return (
    <div>
      <div
        id="scoreEl"
        ref={scoreElRef}
        style={{
          color: "white",
          fontSize: "20px",
          padding: "10px",
          position: "absolute",
          top: "10px",
          right: "10px",
          backgroundColor: "rgba(0,0,0,0.5)",
          borderRadius: "5px",
        }}
      ></div>
      <div
        id="scoreboard"
        ref={scoreboardRef}
        style={{
          color: "white",
          fontSize: "16px",
          padding: "10px",
          position: "absolute",
          top: "10px",
          left: "10px",
          backgroundColor: "rgba(0,0,0,0.5)",
          borderRadius: "5px",
        }}
      ></div>
      <canvas ref={canvasRef} />
      <button onClick={() => window.reloadMap && window.reloadMap()}>
        Reload Map
      </button>
      <GlobalLeaderboard />
    </div>
  );
}

export default App;