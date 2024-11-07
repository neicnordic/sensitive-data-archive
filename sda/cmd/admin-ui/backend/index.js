const express = require('express');
const cors = require('cors');
const app = express();
const https = require('https');

app.use(cors());

const httpsAgent = new https.Agent({ rejectUnauthorized: false });

//TODO Replace with the first test token you obtain from the tokens endpoint
function getToken() {
   return 'eyJ0eXAiOiJKV1QiLCJqa3UiOiJodHRwczovL29pZGM6ODA4MC9qd2siLCJhbGciOiJFUzI1NiIsImtpZCI6Ild5dGFYc2lqRkZKNWhGTGlLWWNPQl9JNVdtQVNsS3lKakdaUTJEeFJwc3cifQ.eyJzdWIiOiJkdW1teUBnZGkuZXUiLCJhdWQiOlsiYXVkMSIsImF1ZDIiXSwiYXpwIjoiYXpwIiwic2NvcGUiOiJvcGVuaWQgZ2E0Z2hfcGFzc3BvcnRfdjEiLCJpc3MiOiJodHRwczovL29pZGM6ODA4MC8iLCJleHAiOjk5OTk5OTk5OTksImlhdCI6MTU2MTYyMTkxMywianRpIjoiNmFkN2FhNDItM2U5Yy00ODMzLWJkMTYtNzY1Y2I4MGMyMTAyIn0.2UcIZ4wmauiMV3arvxIPjHI_zsFEvCOAljEiJUJvhvM4Kudj97QugdgxDKZ1CEu62QAKfx5hF723EXbAtj3IbA'
}

app.get('/api/files', async (req, res) => {
    console.log("Hej backend API")
    try {
        const token = getToken();
        if (!token) return res.status(500).json({ error: 'Failed to obtain token' });

        const apiResponse = await fetch('http://api:8090/files', {
            headers: {
                Authorization: 'Bearer ${token}',
                'Content-Type': 'application/json'
            },
        });
        console.log("API: " + apiResponse)
        res.json(apiResponse.json());
    } catch (error) {
        console.error('Error fetching data from API:', error);
        res.status(500).json({ error: 'Failed to fetch data from API' });
    }
});

app.listen(3000, () => {
    console.log('Backend server running on http://localhost:3000');
});
