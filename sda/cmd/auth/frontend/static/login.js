$.get('/login-options', (options) => {
    options.forEach(option => {
        $("#login-options").append(
            `<div class="col-sm py-3">
      <a href="${option.URL}">
        <img src="public/${option.Name}.png" class="img-fluid">
      </a>
    </div>
    `);
    });
});