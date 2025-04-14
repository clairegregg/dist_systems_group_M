// GlobalLeaderboard.js
import React, { useEffect, useState } from "react";

function GlobalLeaderboard({url, ID}) {
  const [activePlayers, setActivePlayers] = useState([]);
  const [leftPlayers, setLeftPlayers] = useState([]);
  const CENTRAL_URL_ACTIVE = `${url}/scores/active?${ID}` // current chuck URL
  const CENTRAL_URL_LEFT = `${url}/scores/left?${ID}`
  useEffect(() => {
    async function fetchData() {
      try {
        // Fetch active players scores.
        const activeRes = await fetch(CENTRAL_URL_ACTIVE);
        let activeData = await activeRes.json();
        // Sort active players by score descending.
        if (activeData){
          activeData.sort((a, b) => b.score - a.score);
        }
        else{
          activeData = []
        }
        // Fetch left players scores.
        const leftRes = await fetch(CENTRAL_URL_LEFT);
        let leftData = await leftRes.json();

        // Sort left players by score descending.
        if(leftData){
          leftData.sort((a, b) => b.score - a.score);
        }else{
          leftData = []
        }
        setActivePlayers(activeData);
        setLeftPlayers(leftData);
      } catch (error) {
        console.error("Error fetching global leaderboard:", error);
      }
    }
    // Initial fetch and then poll every 1000 ms (1 second).
    fetchData();
    const intervalId = setInterval(fetchData, 1000);
    return () => clearInterval(intervalId);
  }, [CENTRAL_URL_ACTIVE,CENTRAL_URL_LEFT]);

  return (
    <div
      style={{
        border: "1px solid #fff",
        padding: "10px",
        color: "white",
        marginTop: "10px"
      }}
    >
      <h3>Global Leaderboard</h3>
      <div>
        <h4>Active Players ({activePlayers.length})</h4>
        <ul>
          {activePlayers.map((item) => (
            <li key={item.userName}>
              {item.userName}: {item.score}
            </li>
          ))}
        </ul>
      </div>
      <div>
        <h4>Left Players ({leftPlayers.length})</h4>
        <ul>
          {leftPlayers.map((item) => (
            <li key={item.userName}>
              {item.userName}: {item.score}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

export default GlobalLeaderboard;