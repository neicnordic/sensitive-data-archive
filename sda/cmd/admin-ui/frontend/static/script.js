async function fetchDataWithToken() {
  try {
    const response = await fetch('http://localhost:3000/api/files', {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json'
      }
    });


    const data = await response.json();
    return data;

  } catch (error) {
    showAlert('alertMessage','alert-warning','There was an error fetching data.');
    hideTable();
  }
}

async function populateFilesTable() {
  const table = document.getElementById('filesTable');
  table.innerHTML = '';
  let data = await fetchDataWithToken();

  if (Array.isArray(data)) {
    data.forEach(item => {
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
  } else {
    showAlert('alertMessage', 'alert-warning', 'There are no files to display due to an error.');
    hideTable();
  }

  if (data.length === 0) {
    showAlert('alertMessage', 'alert-primary', 'There are no files to display')
    hideTable()
  }

}

function hideTable() {
  document.getElementById('filesTable').style.display = "none";
}

/**
 * Function to show alert
 * @id  the element's id
 * @style  alert styles from Bootstrap f.e alert-primary, alert-warning
 * @message the message to the user
 */
function showAlert(id, style, message) {
  const alert = document.getElementById(id);
  alert.classList.add(style);
  alert.innerHTML = message;
  alert.style.display = 'block';
}

function populateUsersTable() {
  const table = document.getElementById('usersTable');
  const users = ['x@x.com', 'bird@bird.com','dinosaur@dino.com' ];

  users.forEach(user => {
    const row = table.insertRow();
    const cell1 = row.insertCell(0);
    cell1.innerText = user;
  });
}

function matrixRainAnimation() {
  let canvas = document.querySelector('canvas'),
      ctx = canvas.getContext('2d');
  
  canvas.width = window.innerWidth;
  canvas.height = document.querySelector('.navbar').offsetHeight;

  let letters = '01001000 01100101 01101100 01101100 01101111 00100001 01001000 01100101 01101100 01101100 01101111 00100001 01001000 01100101 01101100 01101100 01101111 00100001';
  letters = letters.split('');

  let fontSize = 10,
  columns = canvas.width / fontSize;

  let drops = [];
  for (let i = 0; i < columns; i++) {
    drops[i] = 1;
  }

  function draw() {
    ctx.fillStyle = 'rgba(0, 0, 0, .1)';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    for (let i = 0; i < drops.length; i++) {
      let text = letters[Math.floor(Math.random() * letters.length)];
      ctx.fillStyle = '#0f0';
      ctx.fillText(text, i * fontSize, drops[i] * fontSize);
      drops[i]++;
      if (drops[i] * fontSize > canvas.height && Math.random() > .95) {
        drops[i] = 0;
      }
    }
  }
  setInterval(draw, 100);
}

matrixRainAnimation()
fetchDataWithToken()
populateUsersTable()
populateFilesTable()