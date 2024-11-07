const express = require('express');
const cors = require('cors');
const app = express();
const https = require('https');

app.use(cors());

const httpsAgent = new https.Agent({ rejectUnauthorized: false });

//TODO Replace with the first test token you obtain from the tokens endpoint
function getToken() {
    return 'token';

}

app.get('/api/files', async (req, res) => {
    try {
        const token = getToken();
        if (!token)
            return res.status(500).json({ error: 'Failed to obtain token' });
        const apiResponse = await fetch('http://api:8090/files', {
            headers: {
                Authorization: `Bearer ${token}`,
                'Content-Type': 'application/json'
            },
            agent: httpsAgent
        });

        const responseData = await apiResponse.json();
        res.json(responseData);
    } catch (error) {
        console.error('Error fetching data from API:', error);
        res.status(500).json({ error: 'Failed to fetch data from API' });
    }
});

app.listen(3000, () => {
    console.log('Backend server running on http://localhost:3000');
});
