// GlobalLeaderboard.js
import React, { useEffect, useState } from "react";

function GlobalLeaderboard() {
  const [activePlayers, setActivePlayers] = useState([]);
  const [leftPlayers, setLeftPlayers] = useState([]);
  const CENTRAL_URL_ACTIVE = "http://localhost:8080/scores/active"
  const CENTRAL_URL_LEFT = "http://localhost:8080/scores/left"
  useEffect(() => {
    async function fetchData() {
      try {
        // Fetch active players scores.
        const activeRes = await fetch(CENTRAL_URL_ACTIVE);
        const activeData = await activeRes.json();
        // Sort active players by score descending.
        activeData.sort((a, b) => b.score - a.score);

        // Fetch left players scores.
        const leftRes = await fetch(CENTRAL_URL_LEFT);
        const leftData = await leftRes.json();

        // Sort left players by score descending.
        leftData.sort((a, b) => b.score - a.score);

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
  }, []);

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