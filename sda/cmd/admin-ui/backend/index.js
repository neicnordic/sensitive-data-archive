const express = require('express');
const cors = require('cors');
const app = express();
const https = require('https');

app.use(cors());

const httpsAgent = new https.Agent({ rejectUnauthorized: false });

// See README on how to get the toke
function getToken() {
    return 'token';
}

app.get('/api/files', async (request, response) => {
  try {
      const token = getToken();
      console.log('Token:', token);
      if (!token)
        return response.status(500).json({ error: 'Failed to obtain token' });
      const apiResponse = await fetch('http://api:8090/files', {
        headers: {
            Authorization: `Bearer ${token}`,
            'Content-Type': 'application/json'
        },
        agent: httpsAgent
      });

      const responseData = await apiResponse.json();
      response.json(responseData);
  } catch (error) {
      console.error('Error fetching data from API:', error);
      response.status(500).json({ error: 'Failed to fetch data from API' });
  }
});

app.listen(3000, () => {
    console.log('Backend server running on http://localhost:3000');
});
