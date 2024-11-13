const express = require('express');
const cors = require('cors');
const app = express();
const https = require('https');

app.use(cors());

const httpsAgent = new https.Agent({ rejectUnauthorized: false });

// See README on how to get the token
function getToken() {
    return 'eyJ0eXAiOiJKV1QiLCJqa3UiOiJodHRwczovL29pZGM6ODA4MC9qd2siLCJhbGciOiJFUzI1NiIsImtpZCI6IkJUVUtuekhaQ0hKZTBaa3FraTZNVXN0RC1WeVpIaUhkcXowWVFEWnd1dUEifQ.eyJzdWIiOiJkdW1teUBnZGkuZXUiLCJhdWQiOlsiYXVkMSIsImF1ZDIiXSwiYXpwIjoiYXpwIiwic2NvcGUiOiJvcGVuaWQgZ2E0Z2hfcGFzc3BvcnRfdjEiLCJpc3MiOiJodHRwczovL29pZGM6ODA4MC8iLCJleHAiOjk5OTk5OTk5OTksImlhdCI6MTU2MTYyMTkxMywianRpIjoiNmFkN2FhNDItM2U5Yy00ODMzLWJkMTYtNzY1Y2I4MGMyMTAyIn0.cFa0d6qJFQVheMIwBPn5yO9rz4cN0zF-WuAMujlSxIbtRHsX3JADAH_VBA8Qv3lDafXgxkadKNjD-ww07-wU6w';
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
