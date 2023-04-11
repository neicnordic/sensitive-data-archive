$.get('/login-options', (options) => {
    options.forEach(option => {
        $("#login-options").append(
            `<div class="col-sm py-3">
      <p class="lead">
        For logging in with ${option.Name} please click here:
      </p>
      <a class="btn btn-primary btn-lg btn-block" href="${option.URL}">
        Log in with ${option.Name}
      </a>
    </div>
    `);
    });
});