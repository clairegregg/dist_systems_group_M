function App() {
  const canvas = document.querySelector('canvas');
  const context = canvas.getContext('2d');
  canvas.width = window.innerWidth;
  canvas.height = window.innerHeight;

  // Configuration constants
  const MOVEMENT_SPEED = 5;
  const BOUNDARY_SIZE = 40;
  const PLAYER_RADIUS = 15;

  // --- Class Definitions ---

  class Boundary {
    static width = BOUNDARY_SIZE;
    static height = BOUNDARY_SIZE;

    constructor({ position }) {
      this.position = position;
      this.width = BOUNDARY_SIZE;
      this.height = BOUNDARY_SIZE;
    }

    draw() {
      context.fillStyle = 'blue';
      context.fillRect(this.position.x, this.position.y, this.width, this.height);
    }
  }

  class Player {
    constructor({ id, position, velocity }) {
      this.id = id;
      this.position = position;
      this.velocity = velocity;
      this.radius = PLAYER_RADIUS;
    }

    draw() {
      context.beginPath();
      context.arc(this.position.x, this.position.y, this.radius, 0, Math.PI * 2);
      context.fillStyle = this.isLocal ? 'yellow' : 'red';
      context.fill();
      context.closePath();
    }

    update() {
      this.draw();
      this.position.x += this.velocity.x;
      this.position.y += this.velocity.y;
    }
  }

  // --- Setup Boundaries from Map Data fetched from server ---
  const boundaries = [];

  async function loadMap() {
    try {
      // Adjust the URL as needed; if same-origin, '/getMap' is enough.
      const response = await fetch('http://localhost:8082/getMap');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const mapData = await response.json();
      mapData.forEach((row, rowIndex) => {
        row.forEach((symbol, colIndex) => {
          if (symbol === '1') {
            boundaries.push(
              new Boundary({
                position: {
                  x: Boundary.width * colIndex,
                  y: Boundary.height * rowIndex,
                },
              })
            );
          }
        });
      });
    } catch (error) {
      console.error("Error fetching map data:", error);
    }
  }

  // --- Player Hashmap ---
  const players = new Map();

  // Generate a unique id for the local player.
  const localPlayerId =
    Date.now().toString() + Math.random().toString(36).substring(2);
  const localPlayer = new Player({
    id: localPlayerId,
    position: {
      x: Boundary.width * 1.5,
      y: Boundary.height * 1.5,
    },
    velocity: { x: 0, y: 0 },
  });
  localPlayer.isLocal = true;
  players.set(localPlayerId, localPlayer);

  // --- WebSocket Setup ---
  // Replace with your actual WebSocket server address.
  const socket = new WebSocket("ws://localhost:8082/ws");

  socket.addEventListener("open", () => {
    console.log("WebSocket connection established");
    socket.send(JSON.stringify({ type: "new_player", id: localPlayerId }));
  });

  socket.addEventListener("message", (event) => {
    const data = JSON.parse(event.data);
    if (data.players) {
      const activePlayerIds = new Set(Object.keys(data.players));
      players.forEach((player, playerId) => {
        if (playerId !== localPlayerId && !activePlayerIds.has(playerId)) {
          players.delete(playerId);
        }
      });

      // Update or add remote players.
      Object.keys(data.players).forEach((playerId) => {
        if (playerId === localPlayerId) return;
        const playerData = data.players[playerId];
        if (players.has(playerId)) {
          const remotePlayer = players.get(playerId);
          remotePlayer.position = playerData.position;
          remotePlayer.velocity = playerData.velocity;
        } else {
          const newPlayer = new Player({
            id: playerId,
            position: playerData.position,
            velocity: playerData.velocity,
          });
          newPlayer.isLocal = false;
          players.set(playerId, newPlayer);
        }
      });
    }
  });

  // --- Key Tracking ---
  const keys = {
    w: { pressed: false },
    a: { pressed: false },
    s: { pressed: false },
    d: { pressed: false },
  };
  let lastKey = "";

  window.addEventListener("keydown", ({ key }) => {
    if (keys[key] !== undefined) {
      keys[key].pressed = true;
      lastKey = key;
    }
  });

  window.addEventListener("keyup", ({ key }) => {
    if (keys[key] !== undefined) {
      keys[key].pressed = false;
    }
  });

  // --- Animation Loop ---
  function animate() {
    requestAnimationFrame(animate);
    context.clearRect(0, 0, canvas.width, canvas.height);

    // Draw boundaries.
    boundaries.forEach((boundary) => boundary.draw());

    // Update and render each player.
    players.forEach((player, id) => {
      if (id === localPlayerId) {
        player.velocity.x = 0;
        player.velocity.y = 0;
        if (keys.w.pressed && lastKey === "w") {
          player.velocity.y = -MOVEMENT_SPEED;
        } else if (keys.a.pressed && lastKey === "a") {
          player.velocity.x = -MOVEMENT_SPEED;
        } else if (keys.d.pressed && lastKey === "d") {
          player.velocity.x = MOVEMENT_SPEED;
        } else if (keys.s.pressed && lastKey === "s") {
          player.velocity.y = MOVEMENT_SPEED;
        }

        player.update();
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(
            JSON.stringify({
              id: localPlayerId,
              position: player.position,
              velocity: player.velocity,
            })
          );
        }
      } else {
        player.update();
      }
    });
  }

  // Initialize by loading the map first, then start the animation loop.
  async function init() {
    await loadMap();
    animate();
  }
  init();
}

export default App;