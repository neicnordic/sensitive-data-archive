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
  const token = await getToken();
  if (!token) {
    console.error('No token retrieved');
    return;
  }
  try {
    const response = await fetch('https://oidc:8090/files', {
      method: 'GET', // or POST, PUT, etc., depending on your API
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Access-Control-Allow-Origin': 'no-cors'
      }
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    const data = await response.json();
    console.log('API response:', data);
    return data;
  } catch (error) {
    console.error('Failed to fetch data with token:', error);
  }
}