package no.uio.ifi.localega.doa.services;

import com.networknt.schema.Error;
import com.networknt.schema.InputFormat;
import com.networknt.schema.Schema;
import com.networknt.schema.SchemaLocation;
import com.networknt.schema.SchemaRegistry;
import com.networknt.schema.SchemaRegistryConfig;
import com.networknt.schema.SpecificationVersion;
import com.networknt.schema.regex.GraalJSRegularExpressionFactory;
import no.uio.ifi.localega.doa.exception.JsonSchemaValidationException;
import org.springframework.stereotype.Service;

import java.util.stream.Collectors;

@Service
public class JsonSchemaValidationService {

    private final Schema schema;

    public JsonSchemaValidationService() {
        try {
            SchemaRegistryConfig schemaRegistryConfig = SchemaRegistryConfig.builder()
                    .regularExpressionFactory(GraalJSRegularExpressionFactory.getInstance()).build();

            SchemaRegistry schemaRegistry = SchemaRegistry.withDefaultDialect(SpecificationVersion.DRAFT_7,
                    builder -> builder.schemaRegistryConfig(schemaRegistryConfig));

            this.schema = schemaRegistry.getSchema(SchemaLocation.of("classpath:export-request.json"));
        } catch (Exception e) {
            throw new IllegalStateException("Failed to load JSON schema 'export-request.json' from classpath", e);
        }
    }

    public void validate(String jsonContent) throws JsonSchemaValidationException {
        var errors = schema.validate(
                jsonContent,
                InputFormat.JSON,
                ctx -> ctx.executionConfig(c -> c.formatAssertionsEnabled(true))
        );

        if (!errors.isEmpty()) {
            String errorMessage = errors.stream()
                    .map(Error::getMessage)
                    .collect(Collectors.joining("\n"));
            throw new JsonSchemaValidationException("JSON Schema Validation Failed: " + errorMessage);
        }
    }

}
