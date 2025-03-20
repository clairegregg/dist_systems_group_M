//import Canvas from "./Canvas/Canvas";
import Maps from "./maps"

function App() {
  // Getting the HTML canvas element
  const canvas = document.querySelector('canvas')
  const c = canvas.getContext('2d')

  const scoreEl = document.querySelector('#scoreEl')

  // Setting the width and height of the canvas to match the window sizes
  canvas.width = window.innerWidth
  canvas.height = window.innerHeight

  // Boundary class, used for walls of the level
  class Boundary {
    // Setting the size of each boundary tile
    static width = 40;
    static height = 40;

    constructor({ position }) {
      this.position = position
      this.width = 40
      this.height = 40
    }

    // Draw function for boundary, this displays each boundary on the screen when called
    draw() {
      c.fillStyle = 'blue'
      c.fillRect(this.position.x, this.position.y, this.width, this.height)
    }
  }

  // Player class, used for representing the player within the game
  class Player {
    constructor({ position, velocity }) {
      this.position = position
      this.velocity = velocity
      this.radius = 15
    }

    // Draw function for player, this displays the player as a cirlce
    draw() {
      c.beginPath()
      c.arc(this.position.x, this.position.y, this.radius, 0, Math.PI * 2)
      c.fillStyle = 'yellow'
      c.fill()
      c.closePath()
    }

    // Update function used for movement of the player within the game
    update() {
      this.draw()
      this.position.x += this.velocity.x
      this.position.y += this.velocity.y
    }
  }

  // Pellet class, used for representing the collectable pellets within the game
  class Pellet {
    constructor({ position, velocity }) {
      this.position = position
      this.radius = 3
    }

    // Draw function for pellet, this displays the pellets as a cirlce
    draw() {
      c.beginPath()
      c.arc(this.position.x, this.position.y, this.radius, 0, Math.PI * 2)
      c.fillStyle = 'white'
      c.fill()
      c.closePath()
    }
  }

  const pellets = []
  const boundaries = []

  // Creating the player instance
  const player = new Player({
    position: {
      x: Boundary.width * 1.5,
      y: Boundary.height * 1.5
    },
    velocity: {
      x: 0,
      y: 0
    }
  })

  // Keys list, used for collision detection and movement
  const keys = {
    w: {
      pressed: false
    },
    a: {
      pressed: false
    },
    s: {
      pressed: false
    },
    d: {
      pressed: false
    }
  }

  let score = 0
  let lastKey = ''

  // Using maps from the maps.js file to create the level layout
  Maps.map.forEach((row, i) => {
    row.forEach((symbol, j) => {
      switch (symbol) {
        case '1':
          boundaries.push(
            new Boundary({
              position: {
                x: Boundary.width * j,
                y: Boundary.height * i
              }
            })
          )
          break
        case '0':
          pellets.push(
            new Pellet({
              position: {
                x: Boundary.width * j + Boundary.width/2,
                y: Boundary.height * i + Boundary.height/2
              }
            })
          )
          break
        default:
      }
    })
  })

  // Checks whether the player has collided with a boundary on any side
  function collisionDetection({ player, boundary }) {
    return (player.position.y - player.radius + player.velocity.y <= boundary.position.y + boundary.height &&
      player.position.x + player.radius + player.velocity.x >= boundary.position.x &&
      player.position.y + player.radius + player.velocity.y >= boundary.position.y &&
      player.position.x - player.radius + player.velocity.x <= boundary.position.x + boundary.width)
  }

  // Animates movement of the player and tracks updates for the player and the level
  function animate() {
    requestAnimationFrame(animate)
    c.clearRect(0, 0, canvas.width, canvas.height)

    // Using collision detection for continuous and smooth movement of the player
    if (keys.w.pressed && lastKey === 'w') {
      for (let i = 0; i < boundaries.length; i++) {
        const boundary = boundaries[i]
        if (collisionDetection({
          player: {
            ...player, velocity: {
              x: 0,
              y: -5
            }
          },
          boundary: boundary
        })) {
          player.velocity.y = 0
          break
        } else {
          player.velocity.y = -5
        }
      }
    } else if (keys.a.pressed && lastKey === 'a') {
      for (let i = 0; i < boundaries.length; i++) {
        const boundary = boundaries[i]
        if (collisionDetection({
          player: {
            ...player, velocity: {
              x: -5,
              y: 0
            }
          },
          boundary: boundary
        })) {
          player.velocity.x = 0
          break
        } else {
          player.velocity.x = -5
        }
      }
    } else if (keys.d.pressed && lastKey === 'd') {
      for (let i = 0; i < boundaries.length; i++) {
        const boundary = boundaries[i]
        if (collisionDetection({
          player: {
            ...player, velocity: {
              x: 5,
              y: 0
            }
          },
          boundary: boundary
        })) {
          player.velocity.x = 0
          break
        } else {
          player.velocity.x = 5
        }
      }
    } else if (keys.s.pressed && lastKey === 's') {
      for (let i = 0; i < boundaries.length; i++) {
        const boundary = boundaries[i]
        if (collisionDetection({
          player: {
            ...player, velocity: {
              x: 0,
              y: 5
            }
          },
          boundary: boundary
        })) {
          player.velocity.y = 0
          break
        } else {
          player.velocity.y = 5
        }
      }
    }

    // Pellet collision
    for (let i = pellets.length - 1; 0 < i; i--) {
      const pellet = pellets[i]
      pellet.draw()

      if (Math.hypot(pellet.position.x - player.position.x,
        pellet.position.y - player.position.y) < pellet.radius + player.radius) {
        pellets.splice(i, 1)
        score += 10
        scoreEl.innerHTML = score
      }
    }
    // Draw each boundary object as a wall
    boundaries.forEach((boundary) => {
      boundary.draw()
      if (collisionDetection({ player, boundary })) {
        //console.log('Collision')
        player.velocity.x = 0;
        player.velocity.y = 0;
      }
    })
    // Update the player object for movement
    player.update()
  }

  animate()

  // Check for player input
  window.addEventListener('keydown', ({ key }) => {
    switch (key) {
      case 'w':
        keys.w.pressed = true
        lastKey = 'w'
        break
      case 'a':
        keys.a.pressed = true
        lastKey = 'a'
        break
      case 's':
        keys.s.pressed = true
        lastKey = 's'
        break
      case 'd':
        keys.d.pressed = true
        lastKey = 'd'
        break
      default:
    }
    //console.log(player.velocity)
  })
  window.addEventListener('keyup', ({ key }) => {
    switch (key) {
      case 'w':
        keys.w.pressed = false
        break
      case 'a':
        keys.a.pressed = false
        break
      case 's':
        keys.s.pressed = false
        break
      case 'd':
        keys.d.pressed = false
        break
      default:
    }
    //console.log(player.velocity)
  })
}

export default App;
