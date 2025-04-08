package no.uio.ifi.localega.doa.repositories;

import no.uio.ifi.localega.doa.model.DatasetReferences;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

@Repository
public interface DatasetReferencesRepository extends JpaRepository<DatasetReferences, Integer> {
    DatasetReferences findByReferenceId(String referenceId);
}
