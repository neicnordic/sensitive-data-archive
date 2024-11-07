async function getToken() {
  const url = "https://oidc:8080/tokens";

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


async function getData() {
  const token = await getToken();
  const url = "https://localhost:8080/files";
  try {
    const response = await fetch(url, 
      {
        method: 'GET',
        headers: {
          'Authorization': 'Bearer ' + token
        }}
    );
    if (!response.ok) {
      throw new Error(`Response status: ${response.status}`);
    }

    const json = await response.json();
    console.log(json);

  } catch (error) {
    console.error(error.message);
  }
}

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
    console.log('Fetched data with token:', response);
    return data;
  } catch (error) {
    console.error('Failed to fetch data with token:', error);
  }
}