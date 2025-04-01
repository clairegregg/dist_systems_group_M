import React, { useEffect, useRef } from "react";

function App() {
  const canvasRef = useRef(null);
  const scoreElRef = useRef(null);
  const scoreboardRef = useRef(null);
  const localPlayerIdRef = useRef(
    Date.now().toString() + Math.random().toString(36).substring(2)
  );
  // We'll store the grid offset in a ref.
  const gridOffsetRef = useRef({ x: 0, y: 0 });

  useEffect(() => {
    const canvas = canvasRef.current;
    const scoreEl = scoreElRef.current;
    const scoreboardEl = scoreboardRef.current;
    const c = canvas.getContext("2d");

    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;

    // Flag to indicate that the map has loaded.
    let mapLoaded = false;
    // Queue remote messages received before the map is loaded.
    let remoteMessageQueue = [];

    class Boundary {
      static width = 40;
      static height = 40;
      constructor({ position }) {
        this.position = position;
        this.width = 40;
        this.height = 40;
      }
      draw() {
        c.fillStyle = "blue";
        c.fillRect(this.position.x, this.position.y, this.width, this.height);
      }
    }

    class Player {
      constructor({ id, position, velocity, color, isLocal, score = 0 }) {
        this.id = id;
        this.position = position; // absolute position on canvas
        this.velocity = velocity;
        this.radius = 15;
        this.color = color;
        this.isLocal = isLocal;
        this.score = score;
        // For remote players, we use a target position for interpolation.
        this.targetPosition = { ...position };
      }
      draw() {
        c.beginPath();
        c.arc(this.position.x, this.position.y, this.radius, 0, Math.PI * 2);
        c.fillStyle = this.color;
        c.fill();
        c.closePath();
      }
      update() {
        if (!this.isLocal) {
          // Smoothly interpolate toward the target position.
          this.position.x += (this.targetPosition.x - this.position.x) * 0.3;
          this.position.y += (this.targetPosition.y - this.position.y) * 0.3;
        }
        this.draw();
      }
    }

    class Pellet {
      constructor({ position }) {
        this.position = position; // absolute position on canvas
        this.radius = 3;
        // Unique id based on its absolute position.
        this.id = `${position.x}-${position.y}`;
      }
      draw() {
        c.beginPath();
        c.arc(this.position.x, this.position.y, this.radius, 0, Math.PI * 2);
        c.fillStyle = "white";
        c.fill();
        c.closePath();
      }
    }

    // Hold game objects.
    const remotePlayers = new Map();
    const pellets = [];
    const boundaries = [];

    const localPlayerId = localPlayerIdRef.current;
    const localPlayerColor = `hsl(${Math.random() * 360}, 70%, 60%)`;
    // Initial position will be set in loadMap.
    const localPlayer = new Player({
      id: localPlayerId,
      position: { x: 0, y: 0 },
      velocity: { x: 0, y: 0 },
      color: localPlayerColor,
      isLocal: true,
      score: 0,
    });

    const keys = {
      w: { pressed: false },
      a: { pressed: false },
      s: { pressed: false },
      d: { pressed: false },
    };

    // Create the WebSocket.
    const socket = new WebSocket("ws://localhost:8082/ws");
    // Promise that resolves when the socket is open.
    const socketOpenPromise = new Promise((resolve) => {
      socket.addEventListener("open", () => {
        console.log("WebSocket connected");
        // Send relative position (absolute - gridOffset). (At this point gridOffset is still {0,0}.)
        socket.send(
          JSON.stringify({
            type: "new_player",
            id: localPlayerId,
            color: localPlayerColor,
            score: localPlayer.score,
            position: {
              x: localPlayer.position.x - gridOffsetRef.current.x,
              y: localPlayer.position.y - gridOffsetRef.current.y,
            },
          })
        );
        resolve();
      });
    });

    // Process remote messages in a separate function.
    function processRemoteMessage(messageData) {
      const data = JSON.parse(messageData);
      // Update pellets.
      if (data.pellets) {
        pellets.length = 0;
        data.pellets.forEach((pelletData) => {
          // pelletData.position is relative; convert to absolute.
          const absPos = {
            x: gridOffsetRef.current.x + pelletData.position.x,
            y: gridOffsetRef.current.y + pelletData.position.y,
          };
          pellets.push(new Pellet({ position: absPos }));
        });
      }
      // Update players.
      // Update players state.
if (data.players) {
  Object.entries(data.players).forEach(([id, playerData]) => {
    if (id === localPlayerId) {
      // Update local player's score.
      localPlayer.score = playerData.score || localPlayer.score;
      scoreEl.innerHTML = localPlayer.score;
      return;
    }
    // Convert relative position to absolute.
    const absPos = {
      x: gridOffsetRef.current.x + playerData.position.x,
      y: gridOffsetRef.current.y + playerData.position.y,
    };
    if (!remotePlayers.has(id)) {
      remotePlayers.set(
        id,
        new Player({
          id,
          position: absPos,
          velocity: playerData.velocity,
          color:
            playerData.color ||
            `hsl(${Math.random() * 360}, 70%, 60%)`,
          isLocal: false,
          score: playerData.score || 0,
        })
      );
    } else {
      const remotePlayer = remotePlayers.get(id);
      // Compute the distance between the current and new position.
      const dx = absPos.x - remotePlayer.position.x;
      const dy = absPos.y - remotePlayer.position.y;
      const distance = Math.sqrt(dx * dx + dy * dy);
      const THRESHOLD = 100; // pixels

      // If the difference is too big, snap to the new position.
      if (distance > THRESHOLD) {
        remotePlayer.position = { ...absPos };
        remotePlayer.targetPosition = { ...absPos };
      } else {
        remotePlayer.targetPosition = absPos;
      }
      remotePlayer.velocity = playerData.velocity;
      remotePlayer.score = playerData.score || remotePlayer.score;
    }
  });
}
      updateScoreboard();
    }

    // Register remote message handler.
    socket.addEventListener("message", (event) => {
      // If the map isn't loaded yet, queue the message.
      if (!mapLoaded) {
        remoteMessageQueue.push(event.data);
      } else {
        processRemoteMessage(event.data);
      }
    });

    // Update the scoreboard display.
    function updateScoreboard() {
      let html = `<h3>Players</h3><ul>`;
      html += `<li>${localPlayer.id} (You): ${localPlayer.score}</li>`;
      remotePlayers.forEach((player) => {
        html += `<li>${player.id}: ${player.score}</li>`;
      });
      html += `</ul>`;
      scoreboardEl.innerHTML = html;
    }

    function collisionDetection({ player, boundary, velocity }) {
      const vx = velocity?.x ?? player.velocity.x;
      const vy = velocity?.y ?? player.velocity.y;
      return (
        player.position.y - player.radius + vy <=
          boundary.position.y + boundary.height &&
        player.position.x + player.radius + vx >= boundary.position.x &&
        player.position.y + player.radius + vy >= boundary.position.y &&
        player.position.x - player.radius + vx <=
          boundary.position.x + boundary.width
      );
    }

    async function loadMap() {
      try {
        const res = await fetch("http://localhost:8082/getMap");
        const map = await res.json();
        // Compute grid dimensions and offsets to center the map.
        const rows = map.length;
        const cols = map[0].length;
        const gridWidth = cols * Boundary.width;
        const gridHeight = rows * Boundary.height;
        const offsetX = (canvas.width - gridWidth) / 2;
        const offsetY = (canvas.height - gridHeight) / 2;
        gridOffsetRef.current = { x: offsetX, y: offsetY };
        // Set local player's initial absolute position.
        localPlayer.position = {
          x: offsetX + Boundary.width * 1.5,
          y: offsetY + Boundary.height * 1.5,
        };
        // If any remote players were created before map load, adjust them.
        remotePlayers.forEach((player) => {
          player.position.x += offsetX;
          player.position.y += offsetY;
          player.targetPosition.x += offsetX;
          player.targetPosition.y += offsetY;
        });
        // Build boundaries and pellets.
        map.forEach((row, i) => {
          row.forEach((symbol, j) => {
            if (symbol === "1") {
              boundaries.push(
                new Boundary({
                  position: {
                    x: offsetX + Boundary.width * j,
                    y: offsetY + Boundary.height * i,
                  },
                })
              );
            } else if (symbol === "0") {
              const pellet = new Pellet({
                position: {
                  x: offsetX + Boundary.width * j + Boundary.width / 2,
                  y: offsetY + Boundary.height * i + Boundary.height / 2,
                },
              });
              pellets.push(pellet);
            }
          });
        });
        // Mark map as loaded.
        mapLoaded = true;
        // Process any queued remote messages.
        remoteMessageQueue.forEach((msg) => {
          processRemoteMessage(msg);
        });
        remoteMessageQueue = [];
      } catch (err) {
        console.error("Error loading map:", err);
      }
    }

    let lastSentTime = 0;
    const SEND_INTERVAL = 100;

    function animate() {
      requestAnimationFrame(animate);
      c.clearRect(0, 0, canvas.width, canvas.height);

      let vx = localPlayer.velocity.x;
      let vy = localPlayer.velocity.y;
      if (keys.w.pressed) vy = -6;
      if (keys.s.pressed) vy = 6;
      if (keys.a.pressed) vx = -6;
      if (keys.d.pressed) vx = 6;

      let blockedX = false;
      let blockedY = false;
      for (const boundary of boundaries) {
        if (
          collisionDetection({
            player: localPlayer,
            boundary,
            velocity: { x: vx, y: 0 },
          })
        )
          blockedX = true;
        if (
          collisionDetection({
            player: localPlayer,
            boundary,
            velocity: { x: 0, y: vy },
          })
        )
          blockedY = true;
      }
      localPlayer.velocity.x = blockedX ? 0 : vx;
      localPlayer.velocity.y = blockedY ? 0 : vy;

      // Check collision with pellets.
      for (let i = pellets.length - 1; i >= 0; i--) {
        const pellet = pellets[i];
        pellet.draw();
        if (
          Math.hypot(
            pellet.position.x - localPlayer.position.x,
            pellet.position.y - localPlayer.position.y
          ) < pellet.radius + localPlayer.radius
        ) {
          pellets.splice(i, 1);
          localPlayer.score += 10;
          scoreEl.innerHTML = localPlayer.score;
          updateScoreboard();
          // Inform the server that the pellet was eaten.
          if (socket.readyState === WebSocket.OPEN) {
            socket.send(
              JSON.stringify({
                type: "pellet_eaten",
                pelletId: pellet.id,
                id: localPlayer.id,
                score: localPlayer.score,
              })
            );
          }
        }
      }

      boundaries.forEach((b) => b.draw());

      localPlayer.position.x += localPlayer.velocity.x;
      localPlayer.position.y += localPlayer.velocity.y;
      localPlayer.draw();

      // Send local player state periodically, using relative coordinates.
      if (
        Date.now() - lastSentTime > SEND_INTERVAL &&
        socket.readyState === WebSocket.OPEN
      ) {
        socket.send(
          JSON.stringify({
            id: localPlayer.id,
            position: {
              x: localPlayer.position.x - gridOffsetRef.current.x,
              y: localPlayer.position.y - gridOffsetRef.current.y,
            },
            velocity: localPlayer.velocity,
            color: localPlayer.color,
            score: localPlayer.score,
          })
        );
        lastSentTime = Date.now();
      }

      remotePlayers.forEach((player) => {
        player.update();
      });
    }
    window.addEventListener("keydown", ({ key }) => {
      if (keys[key]) {
        keys[key].pressed = true;
      }
    });
    window.addEventListener("keyup", ({ key }) => {
      if (keys[key]) keys[key].pressed = false;
    });

    // Start animation only after the socket is open and the map is loaded.
    Promise.all([socketOpenPromise, loadMap()]).then(() => {
      animate();
    });
  }, []);

  return (
    <div>
      {/* Score displayed at top-right and scoreboard at top-left */}
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
    </div>
  );
}

export default App;