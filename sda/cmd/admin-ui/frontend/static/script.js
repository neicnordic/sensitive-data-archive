/* async function getToken() {
  const url = "https://localhost:8080/tokens";

  try {
    const response = await fetch(url, {
      method: 'GET', 
      headers: {
        'Access-Control-Allow-Origin': 'no-cors'
      }
    });

    if (!response.ok) {
      throw new Error(`Response status: ${response.status}`);
    }

    const data = await response.json();
    const token = data[0]
    console.log(data[0]);
    return token

  } catch (error) {
    console.error(error);
  }
}
 */


async function fetchDataWithToken() {
  try {
    const response = await fetch('http://localhost:3000/api/files', {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json'
      }
    });

    if (!response.ok) {
      throw new Error(`Response status: ${response.status}`);
    }

    const data = await response.json();
    console.log('Fetched data with token:', data);

    // Parse the response data and update the table
    const table = document.getElementById('filesTable');
    data.forEach(item => { // note the reference to data rather than response
      const row = table.insertRow();
      const cell1 = row.insertCell(0);
      const cell2 = row.insertCell(1);
      const cell3 = row.insertCell(2);

      switch (item.fileStatus.toLowerCase()) {
        case 'uploaded':
        case 'submitted':
        case 'ingested':
        case 'archived':
        case 'verified':
        case 'backed up':
        case 'ready':
          cell2.classList.add('text-success');
          break;
        case 'downloaded':
          cell2.classList.add('text-primary');
          break;
        case 'error':
          cell2.classList.add('text-danger');
          break;
        case 'disabled':
          cell2.classList.add('text-muted');
          break;
        case 'enabled':
          cell2.classList.add('text-info');
          break;
      }
      cell1.innerText = item.createAt;
      cell2.innerText = item.fileStatus;
      cell3.innerText = item.inboxPath;
    });

  } catch (error) {
    console.error('Failed to fetch data with token:', error);
  }
}

document.getElementById('files-tab').addEventListener('click', function () {
  fetchDataWithToken();
});


// MATRIX RAIN ANIMATION
var canvas = document.querySelector('canvas'),
    ctx = canvas.getContext('2d');


canvas.width = window.innerWidth;
canvas.height = document.querySelector('.navbar').offsetHeight;

var letters = '01001000 01100101 01101100 01101100 01101111 00100001 01001000 01100101 01101100 01101100 01101111 00100001 01001000 01100101 01101100 01101100 01101111 00100001';
letters = letters.split('');

var fontSize = 10,
    columns = canvas.width / fontSize;

var drops = [];
for (var i = 0; i < columns; i++) {
  drops[i] = 1;
}

function draw() {
  ctx.fillStyle = 'rgba(0, 0, 0, .1)';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  for (var i = 0; i < drops.length; i++) {
    var text = letters[Math.floor(Math.random() * letters.length)];
    ctx.fillStyle = '#0f0';
    ctx.fillText(text, i * fontSize, drops[i] * fontSize);
    drops[i]++;
    if (drops[i] * fontSize > canvas.height && Math.random() > .95) {
      drops[i] = 0;
    }
  }
}

setInterval(draw, 100);