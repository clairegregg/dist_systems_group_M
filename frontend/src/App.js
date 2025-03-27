import React, { useEffect, useRef } from "react";

function App() {
  const canvasRef = useRef(null);
  const scoreElRef = useRef(null);
  const localPlayerIdRef = useRef(
    Date.now().toString() + Math.random().toString(36).substring(2)
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    const scoreEl = scoreElRef.current;
    const c = canvas.getContext("2d");

    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;

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
      constructor({ id, position, velocity, color, isLocal }) {
        this.id = id;
        this.position = position;
        this.velocity = velocity;
        this.radius = 15;
        this.color = color;
        this.isLocal = isLocal;
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
          this.position.x += (this.targetPosition.x - this.position.x) * 0.2;
          this.position.y += (this.targetPosition.y - this.position.y) * 0.2;
        }
        this.draw();
      }
    }

    class Pellet {
      constructor({ position }) {
        this.position = position;
        this.radius = 3;
      }
      draw() {
        c.beginPath();
        c.arc(
          this.position.x,
          this.position.y,
          this.radius,
          0,
          Math.PI * 2
        );
        c.fillStyle = "white";
        c.fill();
        c.closePath();
      }
    }

    // Use a separate map for remote players.
    const remotePlayers = new Map();
    const pellets = [];
    const boundaries = [];

    const localPlayerId = localPlayerIdRef.current;
    const localPlayerColor = `hsl(${Math.random() * 360}, 70%, 60%)`;
    const localPlayer = new Player({
      id: localPlayerId,
      position: { x: Boundary.width * 1.5, y: Boundary.height * 1.5 },
      velocity: { x: 0, y: 0 },
      color: localPlayerColor,
      isLocal: true,
    });

    const keys = {
      w: { pressed: false },
      a: { pressed: false },
      s: { pressed: false },
      d: { pressed: false },
    };

    let score = 0;

    const socket = new WebSocket("ws://192.168.115.69:8082/ws");

    socket.addEventListener("open", () => {
      console.log("WebSocket connected");
      socket.send(
        JSON.stringify({
          type: "new_player",
          id: localPlayerId,
          color: localPlayerColor,
        })
      );
    });

    socket.addEventListener("message", (event) => {
      const data = JSON.parse(event.data);
      if (data.players) {
        Object.entries(data.players).forEach(([id, playerData]) => {
          // Skip updating local player state from the server.
          if (id === localPlayerId) return;
          if (!remotePlayers.has(id)) {
            remotePlayers.set(
              id,
              new Player({
                id,
                position: playerData.position,
                velocity: playerData.velocity,
                color:
                  playerData.color || `hsl(${Math.random() * 360}, 70%, 60%)`,
                isLocal: false,
              })
            );
          } else {
            const remotePlayer = remotePlayers.get(id);
            // Update target position for smoother interpolation.
            remotePlayer.targetPosition = playerData.position;
            remotePlayer.velocity = playerData.velocity;
          }
        });
      }
    });

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
        const res = await fetch("http://192.168.115.69:8082/getMap");
        const map = await res.json();
        map.forEach((row, i) => {
          row.forEach((symbol, j) => {
            if (symbol === "1") {
              boundaries.push(
                new Boundary({
                  position: {
                    x: Boundary.width * j,
                    y: Boundary.height * i,
                  },
                })
              );
            } else if (symbol === "0") {
              pellets.push(
                new Pellet({
                  position: {
                    x: Boundary.width * j + Boundary.width / 2,
                    y: Boundary.height * i + Boundary.height / 2,
                  },
                })
              );
            }
          });
        });
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

      for (let i = pellets.length - 1; i >= 0; i--) {
        const pellet = pellets[i];
        pellet.draw();
        if (
          Math.hypot(
            pellet.position.x - localPlayer.position.x,
            pellet.position.y - localPlayer.position.y
          ) <
          pellet.radius + localPlayer.radius
        ) {
          pellets.splice(i, 1);
          score += 10;
          scoreEl.innerHTML = score;
        }
      }

      boundaries.forEach((b) => b.draw());

      // Update local player.
      localPlayer.position.x += localPlayer.velocity.x;
      localPlayer.position.y += localPlayer.velocity.y;
      localPlayer.draw();

      // Send local player state periodically.
      if (
        Date.now() - lastSentTime > SEND_INTERVAL &&
        socket.readyState === WebSocket.OPEN
      ) {
        socket.send(
          JSON.stringify({
            id: localPlayerId,
            position: localPlayer.position,
            velocity: localPlayer.velocity,
            color: localPlayer.color,
          })
        );
        lastSentTime = Date.now();
      }

      // Update remote players.
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

    loadMap().then(() => {
      animate();
    });
  }, []);

  return (
    <div>
      <canvas ref={canvasRef} />
      <div id="scoreEl" ref={scoreElRef}></div>
    </div>
  );
}

export default App;