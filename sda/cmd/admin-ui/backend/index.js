const express = require('express');
const cors = require('cors');
const app = express();
const https = require('https');

app.use(cors());

const httpsAgent = new https.Agent({ rejectUnauthorized: false });

// See README on how to get the token
function getToken() {
    return 'eyJ0eXAiOiJKV1QiLCJqa3UiOiJodHRwczovL29pZGM6ODA4MC9qd2siLCJhbGciOiJFUzI1NiIsImtpZCI6Ild5dGFYc2lqRkZKNWhGTGlLWWNPQl9JNVdtQVNsS3lKakdaUTJEeFJwc3cifQ.eyJzdWIiOiJkdW1teUBnZGkuZXUiLCJhdWQiOlsiYXVkMSIsImF1ZDIiXSwiYXpwIjoiYXpwIiwic2NvcGUiOiJvcGVuaWQgZ2E0Z2hfcGFzc3BvcnRfdjEiLCJpc3MiOiJodHRwczovL29pZGM6ODA4MC8iLCJleHAiOjk5OTk5OTk5OTksImlhdCI6MTU2MTYyMTkxMywianRpIjoiNmFkN2FhNDItM2U5Yy00ODMzLWJkMTYtNzY1Y2I4MGMyMTAyIn0.ilCPJ5TWyjG2JK-H7sC7QG0v-PJC_gkuEBY6qSvbZAZcGNC_Jpna-WnF64z-qvf-MosgJiCLlpXoEFfrmFHcFA'; // Replace with your token
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
