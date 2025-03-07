import Canvas from "./Canvas/Canvas";

function App() {
  const draw = (context) => {
    class Boundary {
      constructor({position}) {
          this.position = position
          this.width = 40
          this.height = 40
      }
  
      draw() {
        context.fillStyle = 'blue'
        context.fillRect(this.position.x, this.position.y, this.width, this.height)
      }
    }

    // This map layout is hardcoded for now, but I plan on making a separate JS file just for map layouts to make the code cleaner and easier to work with.
    const map = [
      ['1', '1', '1', '1', '1', '1', '1', '0', '0', '1', '1', '1', '1', '1', '1', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '1'],
      ['1', '0', '1', '1', '1', '0', '1', '1', '1', '1', '0', '1', '1', '1', '0', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0'],
      ['1', '0', '1', '1', '0', '1', '1', '1', '1', '1', '1', '0', '1', '1', '0', '1'],
      ['1', '0', '1', '0', '0', '1', '0', '0', '0', '0', '1', '0', '0', '1', '0', '1'],
      ['1', '0', '1', '0', '1', '1', '1', '1', '1', '1', '1', '1', '0', '1', '0', '1'],
      ['0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0'],
      ['0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0'],
      ['1', '0', '1', '1', '0', '1', '1', '1', '1', '1', '1', '0', '1', '1', '0', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '1'],
      ['1', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '1'],
      ['1', '1', '1', '1', '1', '1', '1', '0', '0', '1', '1', '1', '1', '1', '1', '1']
    ]

    const boundaries = []

    // Setting the coordinates for each wall 
    map.forEach((row, i) => {
      row.forEach((symbol, j) => {
        switch (symbol) {
          case '1':
            boundaries.push(
              new Boundary({
                position: {
                  x: 40 * j,
                  y: 40 * i
                }
              })
            )
            break
          default:
        }
      })
    })

    // Drawing the walls
    boundaries.forEach((boundary) => {
      boundary.draw()
    })
  }

  return <Canvas draw={draw}/>
}

export default App;
